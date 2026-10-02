package services

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAdvertiseArgs struct {
	Services string

	In  string
	Out string
}

// OpAdvertise binds the caller as the provider of a fixed set of services for
// as long as the channel is open. After the ack the node sends services.ask and
// the provider sends services.answer and services.change. Closing the channel,
// or any other object from the provider, ends the binding.
//
// Spec: .ai/system/protocols/services/ops/services.advertise.md
func (mod *Module) OpAdvertise(ctx *astral.Context, q *routing.IncomingQuery, args opAdvertiseArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	// note: ServeApps authorizes advertising the caller itself, never another
	// identity.
	if !mod.Auth.Authorize(ctx, &auth.ServeAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	id := q.Caller()
	// why: the router substitutes the node identity for a caller-less local
	// query, and the node holds ServeApps, so without this check such a query
	// could claim the node's native services.
	if id == nil || id.IsZero() || id.IsEqual(mod.node.Identity()) {
		return q.Reject()
	}

	names, err := services.ParseNames(args.Services)

	ch := q.Accept(channel.WithFormats(args.In, args.Out), channel.WithLockedWrites())
	defer ch.Close()

	if err != nil {
		return ch.Send(astral.Err(err))
	}

	t := &bindingTransport{ch: ch, ready: make(chan struct{})}
	src, err := mod.coord.Register(id, names, t)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	defer src.Close()

	err = ch.Send(&astral.Ack{})
	close(t.ready)
	if err != nil {
		return err
	}

	return ch.Switch(
		channel.WithContext(ctx),
		src.Answer,
		src.Change,
	)
}

// bindingTransport sends asks on an app's advertise channel.
type bindingTransport struct {
	ch *channel.Channel
	// why: Register can queue asks for existing followers before the ack is
	// sent; ready holds them so the ack is always the first object.
	ready chan struct{}
}

func (t *bindingTransport) Send(ask *services.Ask) error {
	<-t.ready
	return t.ch.Send(ask)
}

func (t *bindingTransport) Close() {
	_ = t.ch.Close()
}
