package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/user"
	userClient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/core"
)

type opJoinArgs struct {
	Node string `query:"required"`
	In   string
	Out  string
}

// OpJoin makes this unclaimed node request membership from Node, a node of a
// swarm. The swarm node's join policy decides the request; on admission the
// swarm node sends the contract to this node's user.accept_membership, which
// activates it. The reply is the signed contract.
// Spec: .ai/system/protocols/user/ops/user.join.md
//
// why the origin and caller gates of user.bootstrap: the op claims this node,
// so it admits the same queries.
func (mod *Module) OpJoin(ctx *astral.Context, q *routing.IncomingQuery, args opJoinArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	switch q.Origin() {
	case "", astral.OriginLocal:
	default:
		return ch.Send(astral.NewErrOriginNotAllowed(q.Origin()))
	}

	if mod.ActiveContract() != nil {
		return ch.Send(user.ErrAlreadyClaimed)
	}

	if !q.Caller().IsEqual(mod.node.Identity()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	nodeID, err := mod.Dir.ResolveIdentity(args.Node)
	if err != nil {
		return ch.Send(dir.ErrUnknownIdentity)
	}
	if nodeID.IsZero() {
		return ch.Send(astral.ErrZeroIdentity)
	}
	if nodeID.IsEqual(mod.node.Identity()) {
		return ch.Send(user.ErrJoinSelf)
	}

	// why: the request is this node's own query, and a session without a token
	// has the Network zone stripped (mod/apphost/src/guest.go).
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	node := core.NewClientAs(mod.node, mod.node.Identity())
	signed, err := userClient.New(nodeID, node).RequestMembership(ctx)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(signed)
}
