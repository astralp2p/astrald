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
// user.ExpelAction (auth.ErrNotPermitted otherwise). Only the user is: the swarm
// root handler never answers an expel, so a Privileged node cannot (D7). The user
// key signs the Expulsion, which enters SwarmState.
//
// It admits only a local origin (astral.ErrOriginNotAllowed otherwise): a
// Privileged node may relay as the user (D6), so a networked query with the
// user as caller is no proof of the user's own action, and a node holding a
// software user key would sign the Expulsion for it (D7).
func (mod *Module) OpExpel(ctx *astral.Context, q *routing.IncomingQuery, args opExpelArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

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

	if !mod.Auth.Authorize(ctx, &user.ExpelAction{
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
