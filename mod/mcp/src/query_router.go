package mcp

import (
	"bytes"
	"io"

	authapi "github.com/astralp2p/astral-go/api/auth"
	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// RouteQuery claims queries targeting an agent with a parked astral-listen.
// The query is accepted synchronously — the resolve deadline cannot wait for
// the agent's model — and the live conn becomes a dialog session.
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

	// why before the listener lookup: a parked listener is not permission, and
	// popping one for a query about to be refused would consume it.
	//
	// why the actor is the target and not the caller: the action names what its
	// actor does, and answering is this agent's act. auth walks the contracts the
	// actor is subject to, so naming the caller would search a stranger's
	// delegations for a permission this agent's side holds.
	if !mod.Auth.Authorize(ctx, &mcpapi.AnswerAgentAction{
		Action: authapi.NewAction(q.Target),
		FromID: q.Caller,
	}) {
		return query.RouteNotFound()
	}

	// why: popping the listener atomically makes exactly one query win it;
	// the next astral-listen call parks a fresh one.
	ch, listening := mod.popListener(q.Target)
	if !listening {
		if mod.config.MaxPending <= 0 || mod.pendingCount(q.Target) >= mod.config.MaxPending {
			return query.RouteNotFound()
		}
	}

	path, params := query.Parse(q.QueryString)

	return query.Accept(q, w, func(conn astral.Conn) {
		s := mod.newSession(sessionInfo{
			agent:  q.Target,
			conn:   conn,
			caller: q.Caller,
			path:   path,
			params: params,
			format: sessionFormatRaw,
		})

		// the request payload is whatever arrives inside the read window;
		// query-string-only calls simply send nothing
		msgs, _, _ := s.receive(mod.ctx, mod.config.PayloadReadWindow, mod.config.MaxPayloadBytes, cap(s.ch))
		s.payload = bytes.Join(msgs, nil)

		if listening {
			// note: ch is buffered and exclusively ours — this never blocks
			ch <- s
			return
		}

		mod.enqueuePending(q.Target, s)
	})
}
