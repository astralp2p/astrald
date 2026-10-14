package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetAppRegisterPolicyArgs struct {
	// Identity is the identity or alias that decides apphost.register.
	Identity string
	// Operation is the op the identity serves for the decision.
	Operation string
	In        string
	Out       string
}

// OpSetAppRegisterPolicy selects the app registration policy. Identity and
// operation together delegate the decision; neither restores accept-all; only
// one is an error. It answers Ack once the choice is persisted.
//
// why AdminManageApps: the delegate decides which permits every new app
// receives, which is the administration of app credentials that action covers.
func (mod *Module) OpSetAppRegisterPolicy(ctx *astral.Context, q *routing.IncomingQuery, args opSetAppRegisterPolicyArgs) error {
	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	d := &policy.PolicyDelegate{}

	switch {
	case args.Identity == "" && args.Operation == "":
	case args.Identity == "" || args.Operation == "":
		return ch.Send(astral.NewError("identity and operation are set together"))
	default:
		id, err := mod.Dir.ResolveIdentity(args.Identity)
		if err != nil {
			return ch.Send(astral.Err(err))
		}
		d.Identity = id
		d.Operation = astral.String8(args.Operation)
	}

	if err := mod.SetAppRegisterPolicy(ctx, d); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "app register policy delegate set to %v:%v", d.Identity, d.Operation)

	return ch.Send(&astral.Ack{})
}
