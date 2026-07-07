package nodes

import (
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"sync"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
)

// TorLinkStrategy tries to connect with retries. After quickTimeout it
// signals Done but continues running in background on module context for up to
// backgroundTimeout more. Useful for slow networks like Tor.
type TorLinkStrategy struct {
	mod          *Module
	log          *log.Logger
	network      string
	target       *astral.Identity
	quickRetries int // immediate retries (no delay)
	retries      int // normal retries (with delay)

	quickTimeout      time.Duration
	backgroundTimeout time.Duration

	mu   sync.Mutex
	done chan struct{}
}

var _ nodesmod.LinkStrategy = &TorLinkStrategy{}

func (s *TorLinkStrategy) Name() string { return nodesmod.StrategyTor }

// Signal kicks off a connection attempt; idempotent while one is in flight.
func (s *TorLinkStrategy) Signal(ctx *astral.Context) {
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return
	}
	s.done = make(chan struct{})
	s.mu.Unlock()

	go s.attempt(ctx)
}

func (s *TorLinkStrategy) attempt(ctx *astral.Context) {
	resolvedEndpoints, err := s.mod.ResolveEndpoints(ctx, s.target)
	if err != nil {
		s.log.Logv(2, "%v resolve failed: %v", s.target, err)
		s.signalDone()
		return
	}

	filtered := sig.FilterChan(resolvedEndpoints, func(e *nodes.EndpointWithTTL) bool {
		return e.Network() == s.network
	})

	endpoints := sig.ChanToArray(filtered)
	if len(endpoints) == 0 {
		s.log.Logv(2, "no endpoints found to %v", s.target)
		s.signalDone()
		return
	}

	resultCh := make(chan *Link, 1)

	// Foreground: quick retries only
	workerCtx, workerCancel := ctx.WithTimeout(s.quickTimeout)
	defer workerCancel()

	go func() {
		defer close(resultCh)
		if link := s.try(workerCtx, endpoints, s.quickRetries, false); link != nil {
			resultCh <- link
		}
	}()

	select {
	case link := <-resultCh:
		s.signalDone()
		s.deliverLink(link)
		return
	case <-workerCtx.Done():
		s.signalDone()
		if ctx.Err() != nil {
			return
		}
	}

	bgCtx, bgCancel := s.mod.ctx.WithTimeout(s.backgroundTimeout)
	defer bgCancel()

	bgResultCh := make(chan *Link, 1)
	go func() {
		defer close(bgResultCh)
		if link := s.try(bgCtx, endpoints, s.retries, true); link != nil {
			bgResultCh <- link
		}
	}()

	select {
	case link := <-bgResultCh:
		s.deliverLink(link)
	case <-bgCtx.Done():
	}
}

func (s *TorLinkStrategy) signalDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
}

func (s *TorLinkStrategy) deliverLink(link *Link) {
	if link == nil {
		return
	}

	name := s.Name()
	if !s.mod.linkPool.notifyLinkWatchers(link, &name) {
		link.CloseWithError(nodesmod.ErrExcessLink)
	}
}

func (s *TorLinkStrategy) try(
	ctx *astral.Context,
	endpoints []*nodes.EndpointWithTTL,
	retries int,
	withBackoff bool,
) *Link {

	var backoff *sig.Retry
	if withBackoff {
		b, err := sig.NewRetry(time.Second, time.Minute, 2)
		if err != nil {
			s.log.Logv(2, "failed to create backoff: %v", err)
		}

		backoff = b
	}

	for i := 0; i < retries; i++ {

		for _, ep := range endpoints {
			if ctx.Err() != nil {
				return nil
			}
			if link := s.tryEndpoint(ctx, ep); link != nil {
				return link
			}
		}

		if i < retries-1 {
			if withBackoff {
				delay := backoff.NextDelay()
				s.log.Logv(2, "%v retry %v/%v in %v",
					s.target, i+1, retries, delay)

				select {
				case <-backoff.Retry():
				case <-ctx.Done():
					return nil
				}
			} else {
				s.log.Logv(2, "%v quick retry %v/%v",
					s.target, i+1, retries)
			}
		}
	}

	return nil
}

func (s *TorLinkStrategy) tryEndpoint(ctx *astral.Context, endpoint *nodes.EndpointWithTTL) *Link {
	conn, err := s.mod.Exonet.Dial(ctx, endpoint.Endpoint)
	if err != nil {
		s.log.Logv(2, "%v dial %v: %v", s.target, endpoint, err)
		return nil
	}

	rawLink, err := s.mod.EstablishOutboundLink(ctx, s.target, conn)
	if err != nil {
		s.log.Logv(2, "%v link %v: %v", s.target, endpoint, err)
		conn.Close()
		return nil
	}
	link := rawLink.(*Link)

	// tor is always a candidate for upgrade; monitor for pressure
	link.pressure = NewLinkPressureDetector(time.Now(), TorLinkPressureConfig, func() {
		s.mod.Events.Emit(&nodes.LinkPressureEvent{
			RemoteIdentity: link.RemoteIdentity(),
			LinkID:         link.id,
		})
	})

	s.log.Log("%v linked via %v", s.target, endpoint)
	return link
}

// Done closes once the quick (foreground) phase ends; background retries may
// still be running. Returns an already-closed channel if Signal never ran.
func (s *TorLinkStrategy) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.done
}

// Factory

type TorLinkStrategyConfig struct {
	QuickRetries      int
	Retries           int
	SignalTimeout     time.Duration
	BackgroundTimeout time.Duration
}

type TorLinkStrategyFactory struct {
	mod     *Module
	network string
	config  TorLinkStrategyConfig
}

var _ nodesmod.StrategyFactory = &TorLinkStrategyFactory{}

func (f *TorLinkStrategyFactory) Build(target *astral.Identity) nodesmod.LinkStrategy {
	return &TorLinkStrategy{
		mod:               f.mod,
		log:               f.mod.log.AppendTag(log.Tag("tor")),
		network:           f.network,
		target:            target,
		quickRetries:      f.config.QuickRetries,
		retries:           f.config.Retries,
		quickTimeout:      f.config.SignalTimeout,
		backgroundTimeout: f.config.BackgroundTimeout,
	}
}
