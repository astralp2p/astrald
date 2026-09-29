package messaging

import (
	"errors"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

type opDeleteIdentityArgs struct {
	Identity string `query:"required"`
	Out      string
}

// OpDeleteIdentity removes a participant: revokes every token and grant its
// identity holds, unsets its alias and withdraws its mailbox from this node with
// the mail it owns. The hosting contract is not revoked. Identity takes an
// identity or an alias.
func (mod *Module) OpDeleteIdentity(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteIdentityArgs) error {
	if refusesOrigin(q) {
		return q.Reject()
	}

	// why AdminManageApps, as for apphost's token ops: a participant's
	// credential is an apphost access token, so administering participants is
	// administering tokens.
	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	identity, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.NewError("unknown identity"))
	}

	err = mod.DeleteIdentity(ctx, identity)
	if errors.Is(err, messagingmod.ErrIdentityNotFound) {
		return ch.Send(astral.NewError("identity not found"))
	}
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
