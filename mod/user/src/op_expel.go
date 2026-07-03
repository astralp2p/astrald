package user

import (
	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/astral/channel"
	"github.com/cryptopunkscc/astrald/lib/routing"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/user"
)

type opExpelArgs struct {
	Target string
	In     string `query:"optional"`
	Out    string `query:"optional"`
}

// OpExpel permanently bans the target node from the swarm and returns the signed ban.
// Requires an active contract; the caller must be authorized for user.ExpelAction
// (code 3 otherwise) - the user always is, other identities via authorizers.
func (mod *Module) OpExpel(ctx *astral.Context, q *routing.IncomingQuery, args opExpelArgs) (err error) {
	if mod.ActiveContract() == nil {
		return q.RejectWithCode(2)
	}

	// resolve before authorization - the expel action carries the target
	nodeID, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return q.RejectWithCode(3)
	}

	expel := &user.ExpelAction{Action: auth.NewAction(q.Caller()), Subject: nodeID}
	if !mod.Auth.Authorize(ctx, expel) {
		return q.RejectWithCode(4)
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	signed, err := mod.Expel(ctx, nodeID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}
