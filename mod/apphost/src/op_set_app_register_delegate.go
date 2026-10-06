package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAppRegisterDelegateArgs struct {
	// Delegate is the identity or alias that decides apphost.register. Empty
	// clears the delegate and restores accept-all.
	Delegate string
	In       string
	Out      string
}

// OpSetAppRegisterDelegate sets or clears the identity that decides app
// registration.
//
// why AdminManageApps: the delegate decides which permits every new app
// receives, which is the administration of app credentials that action covers.
func (mod *Module) OpSetAppRegisterDelegate(ctx *astral.Context, q *routing.IncomingQuery, args opSetAppRegisterDelegateArgs) error {
	// why: the delegate is this node's setting, so a caller off a link has no
	// standing to write it.
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if args.Delegate == "" {
		if err := mod.policy.AppRegisterDelegate.Clear(ctx); err != nil {
			return ch.Send(astral.Err(err))
		}
		mod.log.Logv(1, "app register delegate cleared")
		return ch.Send(&astral.Ack{})
	}

	delegate, err := mod.Dir.ResolveIdentity(args.Delegate)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if delegate.IsZero() {
		return ch.Send(astral.NewError("missing identity"))
	}

	if err = mod.policy.AppRegisterDelegate.Set(ctx, delegate); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "app register delegate set to %v", delegate)

	return ch.Send(&astral.Ack{})
}
