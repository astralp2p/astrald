package mcp

import (
	"io"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
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
	// the permission. This agent already wrote to the caller, and a receipt
	// says one thing about that one message. Directions are granted per side,
	// so asking answer_agent_action here would refuse a receipt whenever the
	// two differ — which is the ordinary case, not the edge one.
	if path == mcp.MethodReceipt {
		return mod.acceptReceipt(q, w)
	}

	// why the actor is the target and not the caller: the action names what its
	// actor does, and taking a message is this agent's act. auth walks the
	// contracts the actor is subject to, so naming the caller would search a
	// stranger's delegations for a permission this agent's side holds.
	if !mod.Auth.Authorize(ctx, &mcp.AnswerAgentAction{
		Action: auth.NewAction(q.Target),
		FromID: q.Caller,
	}) {
		return query.RouteNotFound()
	}

	// why every other path is a miss: an agent is a mailbox and not a service.
	// A query naming anything else reaches an agent that does not serve it, and
	// reads as the target being absent.
	if path != mcp.MethodMessage {
		return query.RouteNotFound()
	}

	return mod.acceptMessage(q, w)
}
