package apphost

import (
	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opBindArgs struct {
	Out string
}

// OpBind keeps a session alive so the caller can associate cleanup actions (handler removal) with its lifetime.
// Each BindMsg received during the session registers a token whose handlers are removed on session close.
// apphost ends the session itself when a failed dial removes one of its handlers (endBindSession).
//
// why: no named action guards this op; a session removes only the handlers it
// owns, and a holder of mod.auth.admin_manage_apps_action removes any (bindScope).
func (mod *Module) OpBind(ctx *astral.Context, q *routing.IncomingQuery, args opBindArgs) error {
	// why: the owner is read before accepting and the scope is resolved before
	// the session runs. Accepting drops the en-route entry that names this
	// session, and the cleanup runs from a defer after ctx has ended, where
	// neither the entry nor an authorization call is still available.
	owner := mod.sessionOwner(q)

	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// only local apps can bind
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	scope := bindScope{owner: owner, anyOwner: mod.mayManageApps(ctx, owner)}

	if err := ch.Send(&astral.Ack{}); err != nil {
		return err
	}

	sctx, cancel := ctx.WithCancel()
	defer cancel()

	session := &appBind{scope: scope, cancel: cancel}
	defer mod.finishBind(session)

	err := ch.Switch(
		channel.WithContext(sctx),
		func(msg *apphost.BindMsg) error {
			mod.bindToken(session, msg.Token)
			return nil
		},
	)

	// note: a session apphost ended (endBindSession) is a normal end.
	if sctx.Err() != nil && ctx.Err() == nil {
		return nil
	}
	if err != nil {
		_ = ch.Send(astral.Err(err))
	}
	return err
}
