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
// why the gate is not the reason it may answer a token: the refusal below reads
// the query's origin, and an origin is stamped on two paths only — mod/nodes
// stamps network on a query off a link, and launch stamps mcp on a query an
// agent sends. A query arriving by any other path carries none and is not
// refused. apphost's endpoints are such a path and an agent's PAT authenticates
// there, so an agent reaches this op and reads every tenant's token.
//
// fixme: the op needs a caller-identity check — the node owner, or an identity
// Auth authorizes for agent administration — and the origin refusal narrows
// that rather than standing in for it.
func (mod *Module) OpListAgents(ctx *astral.Context, q *routing.IncomingQuery, args opListAgentsArgs) error {
	if q.Origin() == astral.OriginNetwork {
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
