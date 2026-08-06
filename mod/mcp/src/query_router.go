package mcp

import (
	"bytes"
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// RouteQuery claims queries targeting an agent with a parked astral-listen.
// The query is accepted synchronously — the resolve deadline cannot wait for
// the agent's model — and the live conn becomes a dialog session.
func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	// why: popping the listener atomically makes exactly one query win it;
	// the next astral-listen call parks a fresh one.
	ch, listening := mod.popListener(q.Target)
	if !listening {
		// why: only registered agents queue — every other target must fall
		// through to the other routers immediately.
		if !mod.agentIDs.Contains(q.Target.String()) {
			return query.RouteNotFound()
		}
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
