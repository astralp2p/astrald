package messaging

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCreateIdentityArgs struct {
	Alias    string
	Duration astral.Duration
	Out      string
}

// OpCreateIdentity mints a new participant: a fresh identity with a signed
// relay contract, an optional alias and an access token, with a mailbox this
// node hosts under a hosting contract the identity signs.
//
// The participant it mints takes mail from nobody until something permits it.
// The node holds no reachability of its own, so a participant is reachable
// where a handler, a contract or an external authority says so.
func (mod *Module) OpCreateIdentity(ctx *astral.Context, q *routing.IncomingQuery, args opCreateIdentityArgs) error {
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

	cred, err := mod.CreateIdentity(ctx, args.Alias, args.Duration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(cred)
}
