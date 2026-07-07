package apphost

import (
	"errors"
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// QueryAttachTimeout is how long the host waits for a JS handler to attach a per-query WS
// after receiving an IncomingQueryMsg notification. After this, the inbound query is
// treated as route-not-found.
const QueryAttachTimeout = 5 * time.Second

// errWSHandlerGone is returned by WSHandler.RouteQuery when the registration WS has
// gone away (write failed). The caller removes the handler.
var errWSHandlerGone = errors.New("ws handler gone")

// WSHandler routes inbound queries to a JS app over a registered WS notification
// channel. Each accepted query gets its own per-query WS that the JS app opens after
// receiving IncomingQueryMsg.
type WSHandler struct {
	Identity *astral.Identity
	mod      *Module
	ch       *channel.Channel // notification channel (the registration WS)
}

// pendingInboundQuery tracks an in-flight inbound query awaiting attach. It lives in
// mod.pendingInboundQueries keyed by QueryID, so the attach path can look it up
// when AttachQueryMsg arrives.
type pendingInboundQuery struct {
	query  *astral.InFlightQuery
	attach chan io.ReadWriteCloser // closed/sent to with the per-query conn on accept
	reject chan uint8              // sent with the code on RejectIncomingMsg
}

// RouteQuery pushes IncomingQueryMsg to the registration WS and waits for one of:
//   - per-query WS to attach (carry the conn back through `attach`)
//   - RejectIncomingMsg on the registration WS (carry the code through `reject`)
//   - QueryAttachTimeout elapses → route-not-found
func (h *WSHandler) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	pending := &pendingInboundQuery{
		query:  q,
		attach: make(chan io.ReadWriteCloser, 1),
		reject: make(chan uint8, 1),
	}

	if _, ok := h.mod.pendingInboundQueries.Set(q.Nonce, pending); !ok {
		// extraordinarily unlikely nonce collision
		return query.RouteNotFound()
	}
	defer h.mod.pendingInboundQueries.Delete(q.Nonce)

	err := h.ch.Send(&apphost.IncomingQueryMsg{
		QueryID: q.Nonce,
		Caller:  q.Caller,
		Target:  q.Target,
		Query:   astral.String16(q.QueryString),
	})
	if err != nil {
		return nil, errWSHandlerGone
	}

	timer := time.NewTimer(QueryAttachTimeout)
	defer timer.Stop()

	select {
	case conn := <-pending.attach:
		// proxy bytes from the responder (conn) back to the caller (w)
		go func() {
			io.Copy(w, conn)
			w.Close()
		}()
		return conn, nil

	case code := <-pending.reject:
		return nil, astral.NewErrRejected(code)

	case <-timer.C:
		return query.RouteNotFound()

	case <-ctx.Done():
		return query.RouteNotFound()
	}
}
