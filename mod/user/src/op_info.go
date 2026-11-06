package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opInfoArgs struct {
	Out string
}

// OpInfo returns the node's state and manager, and on a claimed node the
// identity and contract metadata of the active contract.
//
// A fresh or unclaimed node answers State and Manager to every caller the
// apphost gate admits. A claimed node answers only a caller authorized for
// user.SeeSwarmAction (auth.ErrNotPermitted otherwise) - the user and swarm
// members always are, other identities via authorizers.
//
// why no check before the claim: State and Manager are what a setup client
// reads to choose its next step, and neither names a user.
func (mod *Module) OpInfo(ctx *astral.Context, q *routing.IncomingQuery, args opInfoArgs) (err error) {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	info := &user.Info{
		State:   astral.String8(mod.Sys.NodeState(ctx)),
		Manager: mod.Sys.Manager(),
	}

	ac := mod.ActiveContract()
	if ac == nil {
		return ch.Send(info)
	}

	if !mod.Auth.Authorize(ctx, &user.SeeSwarmAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	info.NodeAlias = astral.String8(mod.Dir.DisplayName(ac.Subject))
	info.UserAlias = astral.String8(mod.Dir.DisplayName(ac.Issuer))
	info.ContractID, _ = astral.ResolveObjectID(ac)
	info.Contract = ac
	return ch.Send(info)
}
