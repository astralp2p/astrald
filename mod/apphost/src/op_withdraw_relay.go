package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opWithdrawRelayArgs struct {
	// Identity is the app or the mailbox whose relay contract is taken back.
	Identity string `query:"required"`
	Out      string
}

// OpWithdrawRelay takes back the relay contract (RelayForAction) of an app or a
// mailbox on this node. From the next authorization on this node no longer
// accepts a query relayed for the identity under that contract, and no longer
// attaches it to the identity's outgoing queries.
//
// why: apphost.revoke withdraws grants only; the ten-year contract registration
// issues would otherwise outlive a removed or misbehaving app.
// note: the withdrawal is local. A copy on a sibling stays, and so do the
// identity's access tokens and grants (apphost.delete_token, apphost.revoke).
func (mod *Module) OpWithdrawRelay(ctx *astral.Context, q *routing.IncomingQuery, args opWithdrawRelayArgs) error {
	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why: a withdrawal holds on this node alone, so a caller off a link has no
	// standing to make one.
	if q.Origin() == astral.OriginNetwork {
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	n, err := mod.Auth.WithdrawContracts(identity, &nodes.RelayForAction{})
	if err != nil {
		mod.log.Errorv(1, "error withdrawing relay contract of %v: %v", identity, err)
		return ch.Send(astral.Err(err))
	}
	if n == 0 {
		return ch.Send(astral.NewError("relay contract not found"))
	}

	mod.log.Logv(1, "withdrew %v relay contract(s) of %v", n, identity)

	return ch.Send(&astral.Ack{})
}
