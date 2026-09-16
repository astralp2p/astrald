package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opExpelArgs struct {
	Identity string `query:"required"`
	In       string
	Out      string
}

// OpExpel permanently bans the named node from the swarm and returns the signed ban.
// Requires an active contract; the caller must be authorized for
// user.AdminSwarmAction (code 4 otherwise) - the user always is, other
// identities via authorizers.
func (mod *Module) OpExpel(ctx *astral.Context, q *routing.IncomingQuery, args opExpelArgs) (err error) {
	if mod.ActiveContract() == nil {
		return q.RejectWithCode(2)
	}

	// why: the action carries the node, so resolution runs before authorization.
	// The empty name and "anyone" resolve to the zero identity, which names no
	// node, so the name is refused here instead of banned.
	nodeID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil || nodeID.IsZero() {
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
