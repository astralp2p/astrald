package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/mcp"
)

type opCreateAgentArgs struct {
	Alias    string
	Duration astral.Duration
	Out      string
}

// OpCreateAgent mints a new agent: a fresh identity with a signed relay
// contract, an alias and an access token the agent uses as its PAT.
func (mod *Module) OpCreateAgent(ctx *astral.Context, q *routing.IncomingQuery, args opCreateAgentArgs) (err error) {
	if q.Origin() == astral.OriginNetwork {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	agentID, err := mod.createAgentIdentity(ctx)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	alias, err := mod.assignAlias(agentID, args.Alias)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	dur := args.Duration
	if dur == 0 {
		dur = astral.Duration(mod.config.TokenDuration)
	}

	token, err := mod.Apphost.CreateAccessToken(agentID, dur)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.db.CreateAgent(agentID, alias, string(token.Token), time.Time(token.ExpiresAt))
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	_ = mod.agentIDs.Add(agentID.String())

	mod.log.Logv(1, "created agent %v (%v)", alias, agentID)

	return ch.Send(&mcp.Agent{
		Identity:  agentID,
		Alias:     astral.String8(alias),
		Token:     token.Token,
		ExpiresAt: token.ExpiresAt,
	})
}
