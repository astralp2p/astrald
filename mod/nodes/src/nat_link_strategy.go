package nodes

import (
	"fmt"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	kcpclient "github.com/astralp2p/astral-go/api/kcp/client"
	natclient "github.com/astralp2p/astral-go/api/nat/client"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/mod/nat"
	"github.com/astralp2p/astrald/mod/nodes"
)

type NATLinkStrategy struct {
	mod    *Module
	log    *log.Logger
	target *astral.Identity

	mu   sync.Mutex
	done chan struct{}
}

var _ nodes.LinkStrategy = &NATLinkStrategy{}

func (s *NATLinkStrategy) Name() string { return nodes.StrategyNAT }

// Signal starts a NAT-traversal attempt in the background; a no-op if one is already running.
func (s *NATLinkStrategy) Signal(ctx *astral.Context) {
	s.mu.Lock()
	if s.done != nil {
		s.mu.Unlock()
		return
	}
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer s.signalDone()

		if err := s.attempt(ctx); err != nil {
			s.log.Logv(2, "%v %v", s.target, err)
		}
	}()
}

func (s *NATLinkStrategy) peerSupportsNAT(ctx *astral.Context) bool {
	s.log.Logv(2, "%v checking NAT support", s.target)

	ch, err := servicescli.New(s.target, astrald.Default()).Discover(ctx, false)
	if err != nil {
		s.log.Logv(2, "%v NAT support check failed: %v", s.target, err)
		return false
	}
	for update := range ch {
		if update != nil && string(update.Name) == nat.ModuleName && update.Available {
			s.log.Logv(2, "%v supports NAT traversal", s.target)
			return true
		}
	}
	s.log.Logv(2, "%v does not support NAT traversal", s.target)
	return false
}

func (s *NATLinkStrategy) attempt(ctx *astral.Context) error {
	selfID := s.mod.node.Identity()
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	if !s.peerSupportsNAT(ctx) {
		return fmt.Errorf("target does not support NAT traversal")
	}

	s.log.Log("%v starting traversal", s.target)

	natClient := natclient.New(selfID, astrald.Default())
	hole, err := natClient.Punch(ctx, s.target)
	if err != nil {
		return fmt.Errorf("traverse: %w", err)
	}

	s.log.Log("%v traversal complete, locking hole %v", s.target, hole.Nonce)
	if err := natClient.NodeConsumeHole(ctx, hole.Nonce, s.target); err != nil {
		return fmt.Errorf("consume hole: %w", err)
	}

	s.log.Log("%v hole locked, setting up kcp", s.target)
	local, remote := hole.ActiveEndpoint, hole.PassiveEndpoint
	if hole.PassiveIdentity.IsEqual(selfID) {
		local, remote = hole.PassiveEndpoint, hole.ActiveEndpoint
	}

	peerEndpoint := kcp.Endpoint{
		IP:   remote.IP,
		Port: remote.Port,
	}

	localEndpoint := kcp.Endpoint{
		IP:   local.IP,
		Port: local.Port,
	}

	kcpClient := kcpclient.New(selfID, astrald.Default())

	// note: one flag per created resource, so cleanup undoes only what exists.
	var remoteListener, remoteMapping, localMapping, linked bool

	cleanup := func() {
		cleanupCtx := s.mod.ctx.IncludeZone(astral.ZoneNetwork)
		if localMapping {
			if err := kcpClient.RemoveEndpointLocalPort(cleanupCtx, peerEndpoint); err != nil {
				s.log.Logv(2, "cleanup local socket mapping: %v", err)
			}
		}
		if remoteMapping {
			if err := kcpClient.WithTarget(s.target).RemoveEndpointLocalPort(cleanupCtx, localEndpoint); err != nil {
				s.log.Logv(2, "cleanup remote socket mapping: %v", err)
			}
		}
		if remoteListener {
			if err := kcpClient.WithTarget(s.target).CloseEphemeralListener(cleanupCtx, peerEndpoint.Port); err != nil {
				s.log.Logv(2, "cleanup remote ephemeral listener: %v", err)
			}
		}
	}

	// why: the setup below creates state on the peer, and a failure at any step has
	// to release it. A deferred cleanup covers every early return, including ones
	// added later.
	// why not on success: the link owns that state until it closes, and the watcher
	// goroutine below releases it then.
	defer func() {
		if !linked {
			cleanup()
		}
	}()

	// Set up the remote side: ephemeral listener + endpoint mapping
	err = kcpClient.WithTarget(s.target).CreateEphemeralListener(ctx, peerEndpoint.Port)
	if err != nil {
		return fmt.Errorf("remote create ephemeral listener: %w", err)
	}
	remoteListener = true

	err = kcpClient.WithTarget(s.target).SetEndpointLocalPort(ctx, localEndpoint, peerEndpoint.Port, true)
	if err != nil {
		return fmt.Errorf("remote set endpoint local port: %w", err)
	}
	remoteMapping = true

	err = kcpClient.SetEndpointLocalPort(ctx, peerEndpoint, localEndpoint.Port, true)
	if err != nil {
		return fmt.Errorf("set endpoint local port: %w", err)
	}
	localMapping = true

	s.log.Log("%v dialing %v", s.target, peerEndpoint.Address())
	conn, err := s.mod.Exonet.Dial(ctx, &peerEndpoint)
	if err != nil {
		return fmt.Errorf("dial kcp: %w", err)
	}

	rawLink, err := s.mod.EstablishOutboundLink(ctx, s.target, conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("establish link: %w", err)
	}
	link := rawLink.(*Link)
	linked = true

	go func() {
		<-link.Done()
		cleanup()
	}()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				link.Wake()
			case <-link.Done():
				return
			}
		}
	}()

	s.log.Log("%v linked via %v", s.target, peerEndpoint.Address())
	name := s.Name()
	if !s.mod.linkPool.notifyLinkWatchers(link, &name) {
		link.CloseWithError(nodes.ErrExcessLink)
	}

	return nil
}

func (s *NATLinkStrategy) signalDone() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		close(s.done)
		s.done = nil
	}
}

// Done returns a channel closed when the current attempt finishes; already closed when no attempt is running.
func (s *NATLinkStrategy) Done() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.done == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return s.done
}

// factory

type NATLinkStrategyFactory struct {
	mod *Module
}

var _ nodes.StrategyFactory = &NATLinkStrategyFactory{}

func (f *NATLinkStrategyFactory) Build(target *astral.Identity) nodes.LinkStrategy {
	return &NATLinkStrategy{
		mod:    f.mod,
		log:    f.mod.log.AppendTag("nat"),
		target: target,
	}
}
