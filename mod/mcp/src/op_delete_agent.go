package mcp

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDeleteAgentArgs struct {
	ID  string `query:"required"`
	Out string
}

// OpDeleteAgent removes an agent: revokes its token, unsets its alias and
// deletes its record. ID takes an identity or an alias.
func (mod *Module) OpDeleteAgent(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteAgentArgs) error {
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

	if err = mod.deleteAgent(row); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "deleted agent %v (%v)", row.Alias, row.Identity)

	return ch.Send(&astral.Ack{})
}
