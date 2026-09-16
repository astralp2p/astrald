package mcp

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opDeleteAgentArgs struct {
	Identity string `query:"required"`
	Out      string
}

// OpDeleteAgent removes an agent: revokes its token and its grants, unsets its
// alias and deletes its record. Identity takes an identity or an alias.
func (mod *Module) OpDeleteAgent(ctx *astral.Context, q *routing.IncomingQuery, args opDeleteAgentArgs) error {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	// why AdminManageApps, as for apphost's token ops: an agent's credential is an
	// apphost access token, so administering agents is administering tokens.
	if !mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agentID, err := mod.Dir.ResolveIdentity(args.Identity)
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
