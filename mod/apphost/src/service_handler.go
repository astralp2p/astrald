package apphost

import (
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/query"
)

// QueryAttachTimeout is how long the host waits for a handler to attach a per-query connection
// after receiving an IncomingQueryMsg notification. After this, the inbound query is
// treated as route-not-found.
const QueryAttachTimeout = 5 * time.Second

// errServiceHandlerGone is returned by ServiceHandler.RouteQuery when the registration
// connection has gone away (write failed). The caller removes the handler.
var errServiceHandlerGone = errors.New("service handler gone")

// ServiceHandler routes inbound queries to an app over the connection that app registered
// with register_service_msg. The node pushes an IncomingQueryMsg down that connection and
// the app opens a per-query connection in reply, sending AttachQueryMsg.
//
// Every transport reaches this. onRegisterServiceMsg is dispatched from the switch they
// all share, so the registration is a WebSocket for a browser app and a binary IPC
// connection for a native one; nothing here may assume either.
//
// It is the counterpart of IPCHandler, which inverts the direction: there the app names
// an endpoint and the node dials it per query.
type ServiceHandler struct {
	Identity *astral.Identity
	mod      *Module
	ch       *channel.Channel // notification channel (the registration connection)
}

// pendingInboundQuery tracks an in-flight inbound query awaiting attach. It lives in
// mod.pendingInboundQueries keyed by QueryID, so the attach path can look it up
// when AttachQueryMsg arrives.
type pendingInboundQuery struct {
	query  *astral.InFlightQuery
	attach chan io.ReadWriteCloser // closed/sent to with the per-query conn on accept
	reject chan uint8              // sent with the code on RejectIncomingMsg
}

// RouteQuery pushes IncomingQueryMsg to the registration connection and waits for one of:
//   - a per-query connection to attach (carry the conn back through `attach`)
//   - RejectIncomingMsg on the registration connection (carry the code through `reject`)
//   - QueryAttachTimeout elapses → route-not-found
func (h *ServiceHandler) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
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
		return nil, errServiceHandlerGone
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
