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
// token on purpose, so the operator can recover a lost one. A read the
// dashboard makes per agent is a different question and answers a different
// object.
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
		Visible:   astral.Bool(row.Visible),
		ExpiresAt: astral.Time(row.ExpiresAt),
	})
}
