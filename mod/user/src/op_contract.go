package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opContractArgs struct {
	Out string
}

// OpContract answers a caller directly linked to this node with the node's
// active contract. A caller without a direct link is refused with
// auth.ErrNotPermitted, and then a node without an active contract answers
// user.ErrNoActiveContract.
// Spec: .ai/system/protocols/user/ops/user.contract.md
//
// why: no named action guards this op; a node of the user reads the contract of
// a peer it found nearby before the peer holds its contract, so no swarm permit
// admits it yet. A direct link is the guard.
func (mod *Module) OpContract(ctx *astral.Context, q *routing.IncomingQuery, args opContractArgs) error {
	ch := q.Accept(channel.WithOutputFormat(args.Out))
	defer ch.Close()

	if !mod.Nodes.IsLinked(q.Caller()) {
		return ch.Send(auth.ErrNotPermitted)
	}

	ac := mod.ActiveContract()
	if ac == nil {
		return ch.Send(user.ErrNoActiveContract)
	}

	return ch.Send(ac)
}
