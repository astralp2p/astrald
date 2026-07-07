package apphost

import (
	"errors"
	"io"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/ipc"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// IPCHandler routes queries to a registered IPC endpoint.
type IPCHandler struct {
	Identity *astral.Identity // identity of the handler
	IPCToken astral.Nonce     // token for the IPC endpoint
	Endpoint string           // IPC endpoint
}

var errEndpointUnavailable = errors.New("endpoint unavailable")

// RouteQuery dials the IPC endpoint, forwards the HandleQueryMsg, and pipes both
// directions between the caller and the local app process.
// A new connection is dialled for each query; the endpoint is expected to be a long-running daemon.
func (handler *IPCHandler) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	conn, err := ipc.DialContext(ctx, handler.Endpoint)
	if err != nil {
		return nil, errEndpointUnavailable
	}

	var ch = channel.New(conn)

	err = ch.Send(&apphost.HandleQueryMsg{
		IPCToken: handler.IPCToken,
		ID:       q.Nonce,
		Caller:   q.Caller,
		Target:   q.Target,
		Query:    astral.String16(q.QueryString),
	})
	if err != nil {
		return query.RouteNotFound()
	}

	err = ch.Switch(
		channel.ExpectAck,
		func(msg *apphost.QueryRejectedMsg) error {
			return astral.NewErrRejected(uint8(msg.Code))
		},
		func(msg *apphost.ErrorMsg) error {
			return astral.NewErrRouteNotFound()
		},
		func(object astral.Object) error { // catch all
			return astral.NewErrRouteNotFound()
		},
		func(err error) error {
			return astral.NewErrRouteNotFound()
		},
	)
	if err != nil {
		return nil, err
	}

	// proxy the connection traffic
	go func() {
		io.Copy(w, conn)
		w.Close()
	}()

	return conn, nil
}
