package nodes

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/kcp"
	kcpclient "github.com/astralp2p/astral-go/api/kcp/client"
	"github.com/astralp2p/astral-go/api/nat"
	natclient "github.com/astralp2p/astral-go/api/nat/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/mod/nodes"
)

type NATLinkStrategy struct {
	mod         *Module
	log         *log.Logger
	target      *astral.Identity
	clock       wakeClock                   // the wake loop's time source; nil is the real clock
	roundBudget time.Duration               // 0 is natRoundBudget
	traverse    func(*astral.Context) error // test seam; nil is attempt

	rounds roundKeeper
}

var _ nodes.LinkStrategy = &NATLinkStrategy{}

func (s *NATLinkStrategy) Name() string { return nodes.StrategyNAT }

// Join adds the caller to the running NAT-traversal round, or starts one on the module context.
func (s *NATLinkStrategy) Join(ctx *astral.Context) (release func()) {
	budget := s.roundBudget
	if budget <= 0 {
		budget = natRoundBudget
	}
	return s.rounds.join(ctx, s.mod.ctx, budget, s.run)
}

// run traverses NAT to the target on the round context.
func (s *NATLinkStrategy) run(r *linkRound) {
	traverse := s.traverse
	if traverse == nil {
		traverse = s.attempt
	}
	if err := traverse(r.ctx); err != nil {
		s.log.Logv(2, "%v %v", s.target, err)
	}
}

func (s *NATLinkStrategy) attempt(ctx *astral.Context) error {
	defer s.mod.dials.begin(s.target)()

	selfID := s.mod.node.Identity()
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	// note: a target that does not traverse NAT rejects the punch; no
	// discovery precheck runs, since this node holds no permission to discover
	// the target's services.
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
	ports := newHolePorts(hole, selfID)
	peerEndpoint, peerBindPort := ports.peerEndpoint, ports.peerBindPort
	localEndpoint, localBindPort := ports.localEndpoint, ports.localBindPort

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
			if err := kcpClient.WithTarget(s.target).CloseEphemeralListener(cleanupCtx, peerBindPort); err != nil {
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
	err = kcpClient.WithTarget(s.target).CreateEphemeralListener(ctx, peerBindPort)
	if err != nil {
		return fmt.Errorf("remote create ephemeral listener: %w", err)
	}
	remoteListener = true

	err = kcpClient.WithTarget(s.target).SetEndpointLocalPort(ctx, localEndpoint, peerBindPort, true)
	if err != nil {
		return fmt.Errorf("remote set endpoint local port: %w", err)
	}
	remoteMapping = true

	err = kcpClient.SetEndpointLocalPort(ctx, peerEndpoint, localBindPort, true)
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

	clock := s.clock
	if clock == nil {
		clock = realClock{}
	}
	go natWakeLoop(clock, natKeepalive, link.Wake, link.Done())

	s.log.Log("%v linked via %v", s.target, peerEndpoint.Address())
	s.mod.linkPool.offerStrategyLink(link, s.Name())

	return nil
}

// holePorts is one side's view of a locked hole.
//
// note: an endpoint is the external address the other side observed; a bind
// port is the local UDP port its owner binds. They differ behind a NAT that
// translates ports. kcp binds bind ports and dials external endpoints.
type holePorts struct {
	localEndpoint kcp.Endpoint
	localBindPort astral.Uint16
	peerEndpoint  kcp.Endpoint
	peerBindPort  astral.Uint16
}

func newHolePorts(hole *nat.Hole, self *astral.Identity) holePorts {
	local, remote := hole.ActiveEndpoint, hole.PassiveEndpoint
	if hole.PassiveIdentity.IsEqual(self) {
		local, remote = hole.PassiveEndpoint, hole.ActiveEndpoint
	}

	return holePorts{
		localEndpoint: kcp.Endpoint{IP: local.IP, Port: local.Port},
		localBindPort: astral.Uint16(hole.GetLocalBindPort(self)),
		peerEndpoint:  kcp.Endpoint{IP: remote.IP, Port: remote.Port},
		peerBindPort:  astral.Uint16(hole.GetRemoteBindPort(self)),
	}
}

// Done returns a channel closed when the running round finishes; already closed when no round runs.
func (s *NATLinkStrategy) Done() <-chan struct{} {
	return s.rounds.done()
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
