package nodes

import (
	"context"
	"slices"
	"sync"
	"time"

	nodesmod "github.com/astralp2p/astrald/mod/nodes"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

// basicNetworks are the networks whose rows the basic strategy dials.
var basicNetworks = []string{"tcp", "kcp", "gw"}

// BasicLinkStrategy dials all known endpoints of the target node in parallel
// and uses the first successful connection to establish a link.
type BasicLinkStrategy struct {
	mod         *Module
	networks    []string
	rowBudgets  map[string]time.Duration
	roundBudget time.Duration // 0 is basicRoundBudget
	target      *astral.Identity

	rounds roundKeeper
	// added carries an EndpointsAdded signal to the running round; buffer 1.
	added chan struct{}
}

var _ nodesmod.LinkStrategy = &BasicLinkStrategy{}

func (s *BasicLinkStrategy) Name() string { return nodesmod.StrategyBasic }

// Join adds the caller to the running dialing round, or starts one on the module
// context. The first successful link wins, the rest are closed; the pool then
// settles the winner against the links it already holds.
func (s *BasicLinkStrategy) Join(ctx *astral.Context) (done <-chan struct{}, release func()) {
	budget := s.roundBudget
	if budget <= 0 {
		budget = basicRoundBudget
	}
	return s.rounds.join(ctx, s.mod.ctx, budget, s.run)
}

// basicRound is the dialing state the dials of one basic round share.
type basicRound struct {
	ctx    *astral.Context
	cancel context.CancelFunc // note: ends every dial of the round once a link won
	winner sig.Value[*Link]
	// planned holds network:address of every row the round dialed or queued.
	planned sig.Set[string]
}

// run dials the target's rows on the round context and offers the first link to the pool.
// Rows that EndpointsAdded announces while the first rows dial join the round.
func (s *BasicLinkStrategy) run(r *linkRound) {
	wctx, cancel := r.ctx.WithCancel()
	defer cancel()
	round := &basicRound{ctx: wctx, cancel: cancel}

	// why: a signal sent before this round names rows that the resolve below reads anyway.
	s.drainAdded()

	endpoints, err := s.resolveRows(round)
	if err != nil {
		s.mod.log.Log("resolve endpoints failed: %v", err)
		return
	}

	firstDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dialAdded(round, firstDone)
	}()

	s.dialPlanned(round, endpoints)
	close(firstDone)
	wg.Wait()

	link := round.winner.Get()
	if link == nil {
		return
	}

	s.mod.linkPool.offerStrategyLink(link, s.Name())
}

// EndpointsAdded makes the running round, if any, dial the target's rows it has not planned yet.
// A signal that finds an earlier one pending is absorbed by it.
func (s *BasicLinkStrategy) EndpointsAdded() {
	select {
	case s.added <- struct{}{}:
	default:
	}
}

// drainAdded drops a pending EndpointsAdded signal.
func (s *BasicLinkStrategy) drainAdded() {
	select {
	case <-s.added:
	default:
	}
}

// resolveRows resolves the target's rows and returns, in dial order, those of the
// strategy's networks that the round has not planned, planning each.
// why planned: a row resolved twice in one round, by two resolvers or by a re-resolve, is dialed once.
func (s *BasicLinkStrategy) resolveRows(round *basicRound) (<-chan *dialRow, error) {
	resolved, err := s.mod.ResolveEndpoints(round.ctx, s.target)
	if err != nil {
		return nil, err
	}

	return s.planRows(round.ctx, sig.FilterChan(resolved, func(e *nodes.EndpointWithTTL) bool {
		return slices.Contains(s.networks, e.Network()) && round.planned.Add(e.Network()+":"+e.Address()) == nil
	})), nil
}

// dialPlanned dials rows with DefaultWorkerCount workers and returns once every worker returned.
func (s *BasicLinkStrategy) dialPlanned(round *basicRound, rows <-chan *dialRow) {
	// todo: instead of spawning 8 goroutines per strategy, we could have worker pool which is shared across strategies
	var wg sync.WaitGroup
	wg.Add(DefaultWorkerCount)
	for i := 0; i < DefaultWorkerCount; i++ {
		go func() {
			defer wg.Done()
			s.dialRows(round.ctx, round.cancel, rows, &round.winner)
		}()
	}
	wg.Wait()
}

// dialAdded dials, on every EndpointsAdded signal, the target's rows the round has not planned,
// until stop closes or the round ends. It returns once those dials returned.
//
// why: the round resolves once as it starts, so a row an endpoints-changed pull stores later
// would wait for the next round, after every in-flight dial of a stale row returned: up to the
// tcp dial_timeout. A woken node dials at once (astral-docs protocols/user/README.md).
// why stop: a round whose first rows all returned ends, and the next round resolves afresh.
func (s *BasicLinkStrategy) dialAdded(round *basicRound, stop <-chan struct{}) {
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-round.ctx.Done():
			return
		case <-stop:
			return
		case <-s.added:
		}

		rows, err := s.resolveRows(round)
		if err != nil {
			s.mod.log.Logv(1, "resolve added endpoints failed: %v", err)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.dialPlanned(round, rows)
		}()
	}
}

// dialRows dials rows from endpoints until one links, endpoints closes or ctx ends.
// The first link becomes the winner and cancels the other workers; a later link is excess.
func (s *BasicLinkStrategy) dialRows(ctx *astral.Context, cancel context.CancelFunc, endpoints <-chan *dialRow, winner *sig.Value[*Link]) {
	for {
		select {
		case <-ctx.Done():
			return
		case row, ok := <-endpoints:
			if !ok {
				return
			}

			link := s.tryEndpoint(ctx, row)
			if row.done != nil {
				row.done()
			}
			if link != nil {
				if _, ok := winner.Swap(nil, link); ok {
					cancel()
				} else {
					link.CloseWithError(nodesmod.ErrExcessLink)
				}

				return
			}
		}
	}
}

// tryEndpoint dials one row and establishes a link over it, within the row's budget; nil on any failure.
// The outcome goes to the endpoint health table unless the round is cancelled; see dialLink.
func (s *BasicLinkStrategy) tryEndpoint(ctx *astral.Context, row *dialRow) *Link {
	defer s.mod.dials.begin(s.target)()

	round := ctx
	if d, ok := s.rowBudget(row); ok {
		var cancel context.CancelFunc
		ctx, cancel = ctx.WithTimeout(d)
		defer cancel()
	}

	link, err := s.mod.dialLink(round, ctx, s.target, row.endpoint.Endpoint)
	if err != nil {
		return nil
	}
	return link
}

// rowBudget returns the budget of one dial of row, and false when it has none:
// the network's budget, cut to offSubnetRowBudget for an off-subnet local row.
func (s *BasicLinkStrategy) rowBudget(row *dialRow) (time.Duration, bool) {
	d, ok := s.rowBudgets[row.endpoint.Network()]
	if row.shortBudget(time.Now()) && (!ok || d > offSubnetRowBudget) {
		return offSubnetRowBudget, true
	}
	return d, ok
}

// factory

type BasicLinkStrategyFactory struct {
	mod         *Module
	networks    []string
	rowBudgets  map[string]time.Duration
	roundBudget time.Duration // 0 is basicRoundBudget
}

var _ nodesmod.StrategyFactory = &BasicLinkStrategyFactory{}

// newBasicLinkStrategyFactory returns the factory the module registers: basicNetworks with the row budgets of transportProfiles.
func newBasicLinkStrategyFactory(mod *Module) *BasicLinkStrategyFactory {
	return &BasicLinkStrategyFactory{mod: mod, networks: basicNetworks, rowBudgets: rowBudgetsOf(basicNetworks)}
}

func (f *BasicLinkStrategyFactory) Build(target *astral.Identity) nodesmod.LinkStrategy {
	return &BasicLinkStrategy{
		mod:         f.mod,
		networks:    f.networks,
		rowBudgets:  f.rowBudgets,
		roundBudget: f.roundBudget,
		target:      target,
		added:       make(chan struct{}, 1),
	}
}
