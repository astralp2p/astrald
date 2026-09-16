package apphost

import (
	"errors"
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// RouteQuery dispatches an inbound query to a registered IPC or service handler whose
// identity matches the query target. IPC handlers are tried first; service handlers are
// tried second. An unresponsive or closed handler is automatically removed from the
// registry so stale registrations do not accumulate.
func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	for _, handler := range mod.ipcHandlers.Clone() {
		if !handler.Identity.IsEqual(q.Target) {
			continue
		}

		conn, err := handler.RouteQuery(ctx, q, w)

		// check response
		var rejected *astral.ErrRejected
		switch {
		case err == nil: // accepted
			return conn, nil

		case errors.As(err, &rejected): // rejected
			return query.RejectWithCode(rejected.Code)

		case errors.Is(err, errEndpointUnavailable):
			mod.log.Logv(3, "removing unresponsive query handler at %v", handler.Endpoint)
			mod.ipcHandlers.Remove(handler)
		}
	}

	for _, h := range mod.serviceHandlers.Clone() {
		if !h.Identity.IsEqual(q.Target) {
			continue
		}

		conn, err := h.RouteQuery(ctx, q, w)

		var rejected *astral.ErrRejected
		switch {
		case err == nil:
			return conn, nil
		case errors.As(err, &rejected):
			return query.RejectWithCode(rejected.Code)
		case errors.Is(err, errServiceHandlerGone):
			mod.log.Logv(3, "removing closed service handler for %v", h.Identity)
			mod.serviceHandlers.Remove(h)
		}
	}

	return query.RouteNotFound()
}

// bindScope names whose handlers a bind session removes.
type bindScope struct {
	owner    *astral.Identity // the session that bound; nil for a token-less session
	anyOwner bool             // an administrator removes by token alone
}

// removeHandlersByToken removes the handlers registered under token within scope.
//
// why the owner match: a bind token is a cleanup label the binding app picks
// for itself, so two apps can pick the same one. Matching the owner as well
// keeps one app's bind from removing another app's handlers.
//
// note: a handler registered by a token-less session records no owner, and a
// token-less bind removes exactly those. Neither reaches the other's.
func (mod *Module) removeHandlersByToken(scope bindScope, token astral.Nonce) error {
	for _, h := range mod.ipcHandlers.Clone() {
		if h.IPCToken != token {
			continue
		}

		if !scope.anyOwner && !h.Owner.IsEqual(scope.owner) {
			continue
		}

		mod.ipcHandlers.Remove(h)
	}
	return nil
}
