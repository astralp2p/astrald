package services

import (
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDiscoverArgs struct {
	Services string
	Follow   bool

	In  string
	Out string
}

// OpDiscover evaluates the requested services for the caller and streams the
// offerings. An eos ends the initial attempt, preceded by services.incomplete
// when some provider did not answer. Without follow the channel closes after
// the eos; with follow it carries later views and removals.
//
// A query arriving over a link is served the same way, for the identity that
// sent it. The node answers only from providers it hosts.
//
// Spec: .ai/system/protocols/services/ops/services.discover.md
func (mod *Module) OpDiscover(ctx *astral.Context, q *routing.IncomingQuery, args opDiscoverArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	names, err := services.ParseNames(args.Services)
	if err == nil {
		err = mod.authorizeDiscovery(ctx, q.Caller(), names)
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// fixme: authorization is checked at admission only; a follow outlives a
	// revoked or narrowed grant.
	st := mod.coord.Discover(q.Caller(), names, args.Follow)
	defer st.Close()

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

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			st.Close()
		case <-done:
		}
	}()

	return st.Run(&channelSink{ch: ch})
}

// authorizeDiscovery checks the caller may discover every named service on this
// node. One refusal refuses the request.
func (mod *Module) authorizeDiscovery(ctx *astral.Context, caller *astral.Identity, names []string) error {
	for _, name := range names {
		action := &services.ServiceDiscoveryAction{
			Action:  auth.NewAction(caller),
			Service: astral.String8(name),
			NodeID:  mod.node.Identity(),
		}
		if !mod.Auth.Authorize(ctx, action) {
			return fmt.Errorf("not permitted to discover %q", name)
		}
	}
	return nil
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
