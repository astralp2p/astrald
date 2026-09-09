package mcp

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
)

// RouteQuery answers a delivery or a receipt addressed to an agent this module
// holds.
//
// The node holds no reachability of its own. It holds many tenants' agents and
// knows no relation between them, so which callers an agent answers is asked of
// auth and never decided here.
func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	// why first: only a registered agent is this module's to answer for, and
	// every other target must fall through to the other routers immediately —
	// without reaching an authority that has nothing to say about it.
	if !mod.agentIDs.Contains(q.Target.String()) {
		return query.RouteNotFound()
	}

	path, _ := query.Parse(q.QueryString)

	// why a receipt is admitted without asking the authority: the outbox row is
	// the permission, and directions are granted per side — asking
	// answer_agent_action would refuse a receipt whenever the two differ.
	if path == mcp.MethodReceipt {
		return mod.acceptReceipt(q, w)
	}

	// why the actor is the target and not the caller: auth walks the contracts
	// the actor is subject to, and taking a message is this agent's act.
	//
	// why a rejection and not a route miss: the target is this module's agent,
	// so the answer is terminal, and a code is what tells a sender a refusal
	// from an absence. Which of the authority's reasons it was does not travel:
	// the authority answers one bit and the node holds no more.
	if !mod.Auth.Authorize(ctx, &mcp.AnswerAgentAction{
		Action: auth.NewAction(q.Target),
		FromID: q.Caller,
	}) {
		return query.RejectWithCode(mcpmod.RejectNotAdmitted)
	}

	// why every other path is a miss: an agent is a mailbox and not a service,
	// so a query naming anything else reads as the target being absent.
	if path != mcp.MethodMessage {
		return query.RouteNotFound()
	}

	return mod.acceptMessage(q, w)
}
