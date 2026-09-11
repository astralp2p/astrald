package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListAgentsArgs struct {
	Out string
}

// OpListAgents streams the registered agents, tokens included, so an operator
// can recover a lost PAT.
//
// why the caller is authorized as well as the origin refused: the origin refusal
// reads what the query carries, and an origin is stamped on two paths only —
// mod/nodes stamps network on a query off a link, and launch stamps mcp on a
// query an agent sends. apphost's endpoints stamp none, and an agent's PAT
// authenticates there, so the origin refusal alone lets an agent read every
// tenant's token.
func (mod *Module) OpListAgents(ctx *astral.Context, q *routing.IncomingQuery, args opListAgentsArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	if !mod.authorizeAdminManageApps(ctx, q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agents, err := mod.Agents()
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, a := range agents {
		if err = ch.Send(a); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
