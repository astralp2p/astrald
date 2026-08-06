package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListAgentsArgs struct {
	Out string
}

// OpListAgents streams the registered agents, tokens included — the op is
// local-only, so the operator can recover a lost PAT here.
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
