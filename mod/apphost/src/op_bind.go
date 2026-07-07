package apphost

import (
	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opBindArgs struct {
	Out string `query:"optional"`
}

// OpBind keeps a session alive so the caller can associate cleanup actions (handler removal) with its lifetime.
// Each BindMsg received during the session registers a token whose handlers are removed on session close.
func (mod *Module) OpBind(ctx *astral.Context, q *routing.IncomingQuery, args opBindArgs) error {
	// only local apps can bind
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if err := ch.Send(&astral.Ack{}); err != nil {
		return err
	}

	var actions []func() error
	defer func() {
		for _, a := range actions {
			if err := a(); err != nil {
				mod.log.Error("bind error: %v", err)
			}
		}
	}()

	err := ch.Switch(
		channel.WithContext(ctx),
		func(msg *apphost.BindMsg) error {
			actions = append(actions, func() error {
				return mod.removeHandlersByToken(msg.Token)
			})
			return nil
		},
	)
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
