package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDiscoverArgs struct {
	Services string
	Follow   bool
	Reach    string
	For      string

	In  string
	Out string
}

var (
	errForOverLink     = errors.New("for is accepted only over a link")
	errForIsLocal      = errors.New("a discovery for an app is not carried further")
	errSwarmFromLink   = errors.New("reach=swarm is accepted only from a local app")
	errRelayForRefused = errors.New("not permitted to discover for this app")
)

// OpDiscover evaluates the requested services for the caller and streams the
// offerings. An eos ends the initial attempt, preceded by services.incomplete
// when some contribution did not resolve. Without follow the channel closes
// after the eos; with follow it carries later views and removals.
//
// With reach=swarm the node also carries the discovery to every member of its
// local swarm, in the caller's name. A member receives it as a query from this
// node naming the app in for, and evaluates its own providers for the app.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md
func (mod *Module) OpDiscover(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	d, err := mod.admitDiscovery(ctx, q, args)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// fixme: authorization is checked at admission only; a follow outlives a
	// revoked or narrowed grant.
	st := mod.coord.DiscoverSwarm(d.app, d.names, args.Follow, d.members)
	defer st.Close()

	ctx, cancel := ctx.WithCancel()
	defer cancel()
	for _, r := range st.Remotes() {
		go mod.carry(ctx, r, d.app, d.names, args.Follow)
	}

	go func() {
		// why: the consumer sends nothing; a read ending is how the node learns
		// the consumer closed the stream.
		for {
			if _, err := ch.Receive(); err != nil {
				st.Close()
				return
			}
		}
	}()

	go func() {
		select {
		case <-ctx.Done():
			st.Close()
		case <-st.Done():
		}
	}()

	return st.Run(&channelSink{ch: ch})
}

// discovery is an admitted request: the identity providers evaluate, the
// services, and the swarm members that carry it.
type discovery struct {
	app     *astral.Identity
	names   []string
	members []*astral.Identity
}

// admitDiscovery applies every admission rule before any work starts.
func (mod *Module) admitDiscovery(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs) (*discovery, error) {
	names, err := services.ParseNames(args.Services)
	if err != nil {
		return nil, err
	}
	reach, err := services.ParseReach(args.Reach)
	if err != nil {
		return nil, err
	}
	self := mod.node.Identity()

	if args.For != "" {
		return mod.admitFor(ctx, q, args.For, reach, names)
	}

	d := &discovery{app: q.Caller(), names: names}
	nodeIDs := []*astral.Identity{self}
	if reach == services.ReachSwarm {
		// why: an app asks its own node; a node asked over a link answers
		// from its own providers, so carrying stays one hop.
		if q.Origin() == astral.OriginNetwork {
			return nil, errSwarmFromLink
		}
		d.members = mod.swarmMembers()
		nodeIDs = append(nodeIDs, d.members...)
	}
	if err := mod.authorizeDiscovery(ctx, d.app, names, nodeIDs); err != nil {
		return nil, err
	}
	return d, nil
}

// admitFor admits a swarm member's query carrying an app's discovery: the
// sending node must represent the app and may discover the services here.
func (mod *Module) admitFor(ctx *astral.Context, q *routing.IncomingQuery, forArg string, reach services.Reach, names []string) (*discovery, error) {
	if q.Origin() != astral.OriginNetwork {
		return nil, errForOverLink
	}
	if reach != services.ReachLocal {
		return nil, errForIsLocal
	}
	app, err := astral.ParseIdentity(forArg)
	if err != nil || app.IsZero() {
		return nil, fmt.Errorf("invalid for: %q", forArg)
	}
	if !mod.Auth.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(q.Caller()), ForID: app}) {
		return nil, errRelayForRefused
	}
	if err := mod.authorizeDiscovery(ctx, q.Caller(), names, []*astral.Identity{mod.node.Identity()}); err != nil {
		return nil, err
	}
	return &discovery{app: app, names: names}, nil
}

// authorizeDiscovery checks the actor may discover every named service on
// every node. One refusal refuses the request.
func (mod *Module) authorizeDiscovery(ctx *astral.Context, actor *astral.Identity, names []string, nodeIDs []*astral.Identity) error {
	for _, name := range names {
		for _, nodeID := range nodeIDs {
			action := &services.ServiceDiscoveryAction{
				Action:  auth.NewAction(actor),
				Service: astral.String8(name),
				NodeID:  nodeID,
			}
			if !mod.Auth.Authorize(ctx, action) {
				return fmt.Errorf("not permitted to discover %q", name)
			}
		}
	}
	return nil
}

// swarmMembers are the other members of this node's local swarm.
func (mod *Module) swarmMembers() []*astral.Identity {
	var list []*astral.Identity
	for _, id := range mod.User.LocalSwarm() {
		if !id.IsEqual(mod.node.Identity()) {
			list = append(list, id)
		}
	}
	return list
}

// channelSink writes a discovery stream to the consumer's channel. A write that
// does not finish in writeTimeout closes the channel.
type channelSink struct {
	ch *channel.Channel
}

func (s *channelSink) send(obj astral.Object) error {
	timer := time.AfterFunc(writeTimeout, func() { _ = s.ch.Close() })
	defer timer.Stop()
	return s.ch.Send(obj)
}

func (s *channelSink) Update(u *services.Update) error { return s.send(u) }

func (s *channelSink) Removed(r *services.Removed) error { return s.send(r) }

func (s *channelSink) Boundary(incomplete *services.Incomplete) error {
	if incomplete != nil {
		if err := s.send(incomplete); err != nil {
			return err
		}
	}
	return s.send(&astral.EOS{})
}
