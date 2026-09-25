package mcp

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCreateAgentArgs struct {
	Alias    string
	Duration astral.Duration
	Out      string
}

// OpCreateAgent mints a new agent: a messaging participant with a signed relay
// contract, a signed hosting contract for its mailbox, an alias and an access
// token the agent uses as its PAT, and the agent row that keeps the token for
// list_agents.
//
// The agent it mints answers nobody until something permits a call to it. The
// node holds no reachability of its own, so an agent is reachable where a
// handler, a contract or an external authority says so.
func (mod *Module) OpCreateAgent(ctx *astral.Context, q *routing.IncomingQuery, args opCreateAgentArgs) (err error) {
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

	cred, err := mod.Messaging.CreateIdentity(ctx, args.Alias, args.Duration)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	agent := &mcp.Agent{
		Identity:  cred.Identity,
		Alias:     cred.Alias,
		Token:     cred.Token,
		ExpiresAt: cred.ExpiresAt,
	}

	if err = mod.storeAgent(ctx, agent); err != nil {
		return ch.Send(astral.Err(err))
	}

	mod.log.Logv(1, "created agent %v (%v)", agent.Alias, agent.Identity)

	return ch.Send(agent)
}

// storeAgent records the agent row, and deletes the participant when it cannot.
//
// why the participant goes with a failed row: the row is what list_agents
// answers a lost token from and what delete_agent finds, so a participant
// without one holds a credential no mcp op can reach.
func (mod *Module) storeAgent(ctx *astral.Context, agent *mcp.Agent) error {
	err := mod.db.CreateAgent(&dbAgent{
		Identity:  agent.Identity,
		Alias:     string(agent.Alias),
		Token:     string(agent.Token),
		ExpiresAt: time.Time(agent.ExpiresAt),
	})
	if err == nil {
		return nil
	}

	if derr := mod.Messaging.DeleteIdentity(ctx, agent.Identity); derr != nil {
		mod.log.Error("agent %v: deleting the participant after a failed row: %v", agent.Identity, derr)
	}

	return err
}
