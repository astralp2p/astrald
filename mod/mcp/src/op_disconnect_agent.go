package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDisconnectAgentArgs struct {
	ID  string `query:"required"`
	Out string
}

// OpDisconnectAgent ends an agent's live traffic: it closes the conversations
// the agent is in and drops the queries waiting for it. ID takes an identity or
// an alias.
//
// It carries no policy and writes none. What an agent permits is held by
// whoever owns it, and a change there is answered on the next decision the node
// asks about — this is the one part of that change the owner cannot make for
// itself, because only the node holds the traffic already flowing.
//
// why the parked listener stays: a listener is the agent waiting to be called,
// not a caller it is talking to. Draining it would end the agent's own
// astral-listen, which is not what ending its traffic means.
//
// why local-only: silencing an agent is the account holder's decision, made
// through the dashboard on the node. An operation reachable over the network
// would let any caller end any agent's conversations.
func (mod *Module) OpDisconnectAgent(ctx *astral.Context, q *routing.IncomingQuery, args opDisconnectAgentArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agentID, err := mod.Dir.ResolveIdentity(args.ID)
	if err != nil {
		return ch.Send(astral.NewError("unknown identity"))
	}

	if !mod.agentIDs.Contains(agentID.String()) {
		return ch.Send(astral.NewError("agent not found"))
	}

	mod.dropPending(agentID)
	mod.closeAgentSessions(agentID)

	mod.log.Logv(1, "agent %v disconnected", agentID)

	return ch.Send(&astral.Ack{})
}
