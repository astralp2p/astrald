package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
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
// user.AdminSwarmAction (auth.ErrNotPermitted otherwise) - the user always is, other
// identities via authorizers.
func (mod *Module) OpExpel(ctx *astral.Context, q *routing.IncomingQuery, args opExpelArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if mod.ActiveContract() == nil {
		return ch.Send(user.ErrNoActiveContract)
	}

	// why: the action carries the node, so resolution runs before authorization.
	// The empty name and "anyone" resolve to the zero identity, which names no
	// node, so the name is refused here instead of banned.
	nodeID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if nodeID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}

	if !mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:  auth.NewAction(q.Caller()),
		Subject: nodeID,
	}) {
		return ch.Send(auth.ErrNotPermitted)
	}

	signed, err := mod.Expel(ctx, nodeID)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}
