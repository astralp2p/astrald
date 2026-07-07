package nodes

import (
	"slices"
	"sync"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

// BasicLinkStrategy dials all known endpoints of the target node in parallel
// and uses the first successful connection to establish a link.
type BasicLinkStrategy struct {
	mod      *Module
	networks []string
	target   *astral.Identity

	mu         sync.Mutex
	activeDone chan struct{}
}

var _ nodes.LinkStrategy = &BasicLinkStrategy{}

func (s *BasicLinkStrategy) Name() string { return nodes.StrategyBasic }

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
								link.CloseWithError(nodes.ErrExcessLink)
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
			link.CloseWithError(nodes.ErrExcessLink)
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

func (s *BasicLinkStrategy) tryEndpoint(ctx *astral.Context, endpoint *nodes.EndpointWithTTL) *Link {
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
	mod      *Module
	networks []string
}

var _ nodes.StrategyFactory = &BasicLinkStrategyFactory{}

func (f *BasicLinkStrategyFactory) Build(target *astral.Identity) nodes.LinkStrategy {
	return &BasicLinkStrategy{
		mod:      f.mod,
		networks: f.networks,
		target:   target,
	}
}
