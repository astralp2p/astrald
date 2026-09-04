package mcp

import (
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opAgentArgs struct {
	ID  string `query:"required"`
	Out string
}

// OpAgent answers one agent's record without its access token. ID takes an
// identity or an alias.
//
// why not a filter on mcp.list_agents: that op streams every agent with its
// token, which is how a lost one is recovered. A read made per agent is a
// different question and answers a different object.
//
// why this op answers no token: what reaches mcp.list_agents reaches this one —
// the refusal below reads the query's origin, which no caller on the node's own
// entry paths carries — so the token is left out of the object rather than out
// of reach. See OpListAgents for what the refusal covers.
func (mod *Module) OpAgent(ctx *astral.Context, q *routing.IncomingQuery, args opAgentArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agentID, err := mod.Dir.ResolveIdentity(args.ID)
	if err != nil {
		return ch.Send(astral.NewError("unknown identity"))
	}

	row, err := mod.db.FindAgent(agentID)
	if err != nil {
		return ch.Send(astral.NewError("agent not found"))
	}

	return ch.Send(&mcp.AgentInfo{
		Identity:  row.Identity,
		Alias:     astral.String8(row.Alias),
		ExpiresAt: astral.Time(row.ExpiresAt),
	})
}
