package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetExposedArgs struct {
	ID      string `query:"required"`
	Exposed bool   `query:"required"`
	Out     string
}

// OpSetExposed opens or closes an agent to callers other than itself. ID takes
// an identity or an alias.
//
// why local-only: exposure is the account holder's decision, made through the
// dashboard on the node. An agent deciding its own reachability would let one
// tenant's compromised agent open itself to the network.
func (mod *Module) OpSetExposed(ctx *astral.Context, q *routing.IncomingQuery, args opSetExposedArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agentID, err := mod.Dir.ResolveIdentity(args.ID)
	if err != nil {
		return ch.Send(astral.NewError("unknown identity"))
	}

	if err = mod.db.SetExposed(agentID, args.Exposed); err != nil {
		return ch.Send(astral.NewError("agent not found"))
	}

	// why the store first: the mirror is what the router reads, so a mirror
	// ahead of a failed write would route on a decision nothing recorded.
	if args.Exposed {
		_ = mod.exposed.Add(agentID.String())
	} else {
		_ = mod.exposed.Remove(agentID.String())
		// why the sessions go: closing an agent that a caller is already
		// talking to leaves the conversation the flag was meant to end.
		mod.dropPending(agentID)
		mod.closeAgentSessions(agentID)
	}

	mod.log.Logv(1, "agent %v exposed=%v", agentID, args.Exposed)

	return ch.Send(&astral.Ack{})
}
