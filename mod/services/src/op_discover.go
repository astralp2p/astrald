package services

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/services/src/coordinator"
)

type opDiscoverArgs struct {
	Services string
	Follow   bool
	Reach    string
	For      string
	Tagged   bool

	In  string
	Out string
}

var (
	errForOverLink        = errors.New("for is accepted only over a link")
	errForIsLocal         = errors.New("a discovery for an app is not carried further")
	errSwarmFromLink      = errors.New("reach=swarm is accepted only from a local app")
	errRelayForRefused    = errors.New("not permitted to discover for this app")
	errFollowWithoutNames = errors.New("follow requires services")
	errTaggedWithoutFor   = errors.New("tagged is accepted only with for")
)

// OpDiscover evaluates services for the caller and streams the offerings the
// caller may see. An eos ends the initial attempt, preceded by
// services.incomplete when some contribution did not resolve. Without follow
// the channel closes after the eos; with follow it carries later views and
// removals. A request naming services opens with services.not_granted when
// the caller lacks a permit for some of them.
//
// With reach=swarm the node also carries the discovery to every member of its
// local swarm, in the caller's name. A member receives it as a query from this
// node naming the app in for, evaluates its own providers for the app, and
// tags each offering with its visibility; this node filters.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md
func (mod *Module) OpDiscover(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	d, err := mod.admitDiscovery(ctx, q, args)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if len(d.notGranted) > 0 {
		if err := ch.Send(notGranted(d.notGranted)); err != nil {
			return err
		}
	}

	// fixme: authorization is checked at admission only; a follow outlives a
	// revoked or narrowed grant.
	st := mod.coord.Discover(d.request)
	defer st.Close()

	ctx, cancel := ctx.WithCancel()
	defer cancel()
	// why: with All the request's names are this node's snapshot; a member is
	// asked for every service of its own.
	memberNames := d.request.Names
	if d.request.All {
		memberNames = nil
	}
	for _, r := range st.Contributions() {
		go mod.carry(ctx, memberCarry{contribution: r, view: d.view, names: memberNames, follow: args.Follow})
	}

	go closeWhenConsumerLeaves(ch, st)
	go closeOnCancel(ctx, st)

	return st.Run(&channelSink{ch: ch})
}

// closeWhenConsumerLeaves closes the stream once the consumer closes its end of
// the channel.
func closeWhenConsumerLeaves(ch *channel.Channel, st *coordinator.Stream) {
	// why: the consumer sends nothing; a read ending is how the node learns
	// the consumer closed the stream.
	for {
		if _, err := ch.Receive(); err != nil {
			st.Close()
			return
		}
	}
}

// closeOnCancel closes the stream when the op's context ends first.
func closeOnCancel(ctx *astral.Context, st *coordinator.Stream) {
	select {
	case <-ctx.Done():
		st.Close()
	case <-st.Done():
	}
}

// discovery is an admitted request: the stream to enroll, the view that
// filters swarm members' offerings for its caller, and the requested services
// the caller is not granted.
type discovery struct {
	request    coordinator.Request
	view       *callerView
	notGranted []string
}

// admitDiscovery applies every admission rule before any work starts.
// Permits do not refuse a caller; they decide what it sees.
func (mod *Module) admitDiscovery(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs) (*discovery, error) {
	var names []string
	if args.Services != "" {
		parsed, err := services.ParseNames(args.Services)
		if err != nil {
			return nil, err
		}
		names = parsed
	} else if args.Follow {
		return nil, errFollowWithoutNames
	}
	reach, err := services.ParseReach(args.Reach)
	if err != nil {
		return nil, err
	}

	if args.For != "" {
		return mod.admitCarry(ctx, q, args, names)
	}
	if args.Tagged {
		return nil, errTaggedWithoutFor
	}

	req := coordinator.Request{Caller: q.Caller(), Names: names, Follow: args.Follow}
	nodeIDs := []*astral.Identity{mod.node.Identity()}
	if reach == services.ReachSwarm {
		// why: an app asks its own node; a node asked over a link answers
		// from its own providers, so carrying stays one hop.
		if q.Origin() == astral.OriginNetwork {
			return nil, errSwarmFromLink
		}
		req.Members = mod.swarmMembers()
		nodeIDs = append(nodeIDs, req.Members...)
	}

	d := &discovery{view: mod.newCallerView(ctx, req.Caller)}
	if len(names) == 0 {
		req.All = true
		req.Names = mod.coord.Names()
	} else {
		d.notGranted = d.view.notGranted(ctx, names, nodeIDs)
	}
	req.Filter = d.view.localFilter(ctx, req.Names)
	d.request = req
	return d, nil
}

// admitCarry admits a swarm member's query carrying an app's discovery: the
// sending node must represent the app and may discover the services here. The
// stream is not filtered; the sending node filters for its caller.
func (mod *Module) admitCarry(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs, names []string) (*discovery, error) {
	if q.Origin() != astral.OriginNetwork {
		return nil, errForOverLink
	}
	if reach, _ := services.ParseReach(args.Reach); reach != services.ReachLocal {
		return nil, errForIsLocal
	}
	app, err := astral.ParseIdentity(args.For)
	if err != nil || app.IsZero() {
		return nil, fmt.Errorf("invalid for: %q", args.For)
	}
	if !mod.Auth.Authorize(ctx, &nodes.RelayForAction{Action: auth.NewAction(q.Caller()), ForID: app}) {
		return nil, errRelayForRefused
	}

	req := coordinator.Request{Caller: app, Names: names, Follow: args.Follow, Tagged: args.Tagged}
	if len(names) == 0 {
		// why: without names there is no service to authorize; the sending
		// node's swarm membership grants discovery of every service here.
		if !slices.ContainsFunc(mod.swarmMembers(), q.Caller().IsEqual) {
			return nil, errRelayForRefused
		}
		req.All = true
		req.Names = mod.coord.Names()
	} else if err := mod.authorizeDiscovery(ctx, q.Caller(), names); err != nil {
		return nil, err
	}
	return &discovery{request: req}, nil
}

// authorizeDiscovery checks the actor may discover every named service on this
// node. One refusal refuses the request.
func (mod *Module) authorizeDiscovery(ctx *astral.Context, actor *astral.Identity, names []string) error {
	for _, name := range names {
		action := &services.ServiceDiscoveryAction{
			Action:  auth.NewAction(actor),
			Service: astral.String8(name),
			NodeID:  mod.node.Identity(),
		}
		if !mod.Auth.Authorize(ctx, action) {
			return fmt.Errorf("not permitted to discover %q", name)
		}
	}
	return nil
}

func notGranted(names []string) *services.NotGranted {
	ng := &services.NotGranted{}
	for _, name := range names {
		ng.Services = append(ng.Services, astral.String8(name))
	}
	return ng
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

func (s *channelSink) TaggedUpdate(u *services.TaggedUpdate) error { return s.send(u) }

func (s *channelSink) Removed(r *services.Removed) error { return s.send(r) }

func (s *channelSink) Boundary(incomplete *services.Incomplete) error {
	if incomplete != nil {
		if err := s.send(incomplete); err != nil {
			return err
		}
	}
	return s.send(&astral.EOS{})
}
