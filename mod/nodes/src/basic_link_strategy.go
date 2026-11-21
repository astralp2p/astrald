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

// run dials the target's rows on the round context and offers the first link to the pool.
func (s *BasicLinkStrategy) run(r *linkRound) {
	var wg sync.WaitGroup
	var winner sig.Value[*Link]

	wctx, cancel := r.ctx.WithCancel()
	defer cancel()

	resolved, err := s.mod.ResolveEndpoints(wctx, s.target)
	if err != nil {
		s.mod.log.Log("resolve endpoints failed: %v", err)
		return
	}

	endpoints := s.planRows(wctx, sig.FilterChan(resolved, func(e *nodes.EndpointWithTTL) bool {
		return slices.Contains(s.networks, e.Network())
	}))

	// todo: instead of spawning 8 goroutines per strategy, we could have worker pool which is shared across strategies
	wg.Add(DefaultWorkerCount)
	for i := 0; i < DefaultWorkerCount; i++ {
		go func() {
			defer wg.Done()
			s.dialRows(wctx, cancel, endpoints, &winner)
		}()
	}

	wg.Wait()

	link := winner.Get()
	if link == nil {
		return
	}

	s.mod.linkPool.offerStrategyLink(link, s.Name())
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
// The outcome goes to the endpoint health table unless the round ended first.
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
	}
}
