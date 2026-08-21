package apphost

import (
	"errors"
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// RouteQuery dispatches an inbound query to a registered IPC or WS handler whose
// identity matches the query target. IPC handlers are tried first; WS handlers are
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

	for _, h := range mod.wsHandlers.Clone() {
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
		case errors.Is(err, errWSHandlerGone):
			mod.log.Logv(3, "removing closed ws handler for %v", h.Identity)
			mod.wsHandlers.Remove(h)
		}
	}

	return query.RouteNotFound()
}

func (mod *Module) removeHandlersByToken(token astral.Nonce) error {
	for _, h := range mod.ipcHandlers.Clone() {
		if h.IPCToken == token {
			mod.ipcHandlers.Remove(h)
		}
	}
	return nil
}
