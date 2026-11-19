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

// defaultRowBudgets bounds one row's dial and handshake per network; a network without an entry has no bound of its own.
// why: a kcp dial succeeds at once over UDP, so a dead row fails only in the Noise handshake, which nothing else bounds.
// why: a gw dial routes a query to the gateway, which may start a nested dial round for the gateway itself.
var defaultRowBudgets = map[string]time.Duration{
	"kcp": 5 * time.Second,
	"gw":  20 * time.Second,
}

// BasicLinkStrategy dials all known endpoints of the target node in parallel
// and uses the first successful connection to establish a link.
type BasicLinkStrategy struct {
	mod        *Module
	networks   []string
	rowBudgets map[string]time.Duration
	target     *astral.Identity

	mu         sync.Mutex
	activeDone chan struct{}
}

var _ nodesmod.LinkStrategy = &BasicLinkStrategy{}

func (s *BasicLinkStrategy) Name() string { return nodesmod.StrategyBasic }

// Signal starts a dialing round in the background; concurrent calls while a round
// is still active are ignored. The first successful link wins, the rest are closed.
func (s *BasicLinkStrategy) Signal(ctx *astral.Context) {
	s.mu.Lock()
	if s.activeDone != nil {
		s.mu.Unlock()
		return
	}

	s.activeDone = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			close(s.activeDone)
			s.activeDone = nil
			s.mu.Unlock()
		}()

		var wg sync.WaitGroup
		var winner sig.Value[*Link]

		wctx, cancel := ctx.WithCancel()
		defer cancel()

		resolved, err := s.mod.ResolveEndpoints(wctx, s.target)
		if err != nil {
			s.mod.log.Log("resolve endpoints failed: %v", err)
			return
		}

		endpoints := sig.FilterChan(resolved, func(e *nodes.EndpointWithTTL) bool {
			return slices.Contains(s.networks, e.Network())
		})

		// todo: instead of spawning 8 goroutines per strategy, we could have worker pool which is shared across strategies
		wg.Add(DefaultWorkerCount)
		for i := 0; i < DefaultWorkerCount; i++ {
			go func() {
				defer wg.Done()
				for {
					select {
					case <-wctx.Done():
						return
					case re, ok := <-endpoints:
						if !ok {
							return
						}

						link := s.tryEndpoint(wctx, re)
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
			}()
		}

		wg.Wait()

		link := winner.Get()
		if link == nil {
			return
		}

		name := s.Name()
		if !s.mod.linkPool.notifyLinkWatchers(link, &name) {
			link.CloseWithError(nodesmod.ErrExcessLink)
		}
	}()
}

// Done returns a channel closed when the active round finishes; a closed channel
// is returned immediately when no round is running.
func (s *BasicLinkStrategy) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeDone == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.activeDone
}

// tryEndpoint dials one row and establishes a link over it, within the row's network budget; nil on any failure.
func (s *BasicLinkStrategy) tryEndpoint(ctx *astral.Context, endpoint *nodes.EndpointWithTTL) *Link {
	if d, ok := s.rowBudgets[endpoint.Network()]; ok {
		var cancel context.CancelFunc
		ctx, cancel = ctx.WithTimeout(d)
		defer cancel()
	}

	conn, err := s.mod.Exonet.Dial(ctx, endpoint.Endpoint)
	if err != nil {
		return nil
	}

	rawLink, err := s.mod.EstablishOutboundLink(ctx, s.target, conn)
	if err != nil {
		conn.Close()
		return nil
	}
	link := rawLink.(*Link)

	return link
}

// factory

type BasicLinkStrategyFactory struct {
	mod        *Module
	networks   []string
	rowBudgets map[string]time.Duration
}

var _ nodesmod.StrategyFactory = &BasicLinkStrategyFactory{}

// newBasicLinkStrategyFactory returns the factory the module registers: basicNetworks with defaultRowBudgets.
func newBasicLinkStrategyFactory(mod *Module) *BasicLinkStrategyFactory {
	return &BasicLinkStrategyFactory{mod: mod, networks: basicNetworks, rowBudgets: defaultRowBudgets}
}

func (f *BasicLinkStrategyFactory) Build(target *astral.Identity) nodesmod.LinkStrategy {
	return &BasicLinkStrategy{
		mod:        f.mod,
		networks:   f.networks,
		rowBudgets: f.rowBudgets,
		target:     target,
	}
}
