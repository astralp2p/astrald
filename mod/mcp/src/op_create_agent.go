package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCreateAgentArgs struct {
	Alias    string
	Duration astral.Duration
	Visible  bool
	Out      string
}

// OpCreateAgent mints a new agent: a fresh identity with a signed relay
// contract, an alias and an access token the agent uses as its PAT. Visible
// opens the agent to other callers in the same write; omitted, the agent is
// closed and mcp.set_visible opens it later.
//
// why the argument grants nothing new: create_agent and set_visible are both
// local-only, so the caller that mints an agent may already open it. The
// argument saves the second call, and its false default leaves a caller that
// omits it the closed agent it minted before.
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

	err = mod.registerAgent(&dbAgent{
		Identity:  agentID,
		Alias:     alias,
		Token:     string(token.Token),
		ExpiresAt: time.Time(token.ExpiresAt),
		Visible:   args.Visible,
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "created agent %v (%v) visible=%v", alias, agentID, args.Visible)

	return ch.Send(&mcp.Agent{
		Identity:  agentID,
		Alias:     astral.String8(alias),
		Token:     token.Token,
		ExpiresAt: token.ExpiresAt,
	})
}
