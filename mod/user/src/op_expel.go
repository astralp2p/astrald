package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opExpelArgs struct {
	Target string `query:"required"`
	In     string
	Out    string
}

// OpExpel permanently bans the target node from the swarm and returns the signed ban.
// Requires an active contract; the caller must be authorized for
// user.AdminSwarmAction (code 4 otherwise) - the user always is, other
// identities via authorizers.
func (mod *Module) OpExpel(ctx *astral.Context, q *routing.IncomingQuery, args opExpelArgs) (err error) {
	if mod.ActiveContract() == nil {
		return q.RejectWithCode(2)
	}

	// resolve before authorization - the action carries the target
	nodeID, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return q.RejectWithCode(3)
	}

	if !mod.authorizeAdminSwarm(ctx, q, nodeID, nil) {
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
