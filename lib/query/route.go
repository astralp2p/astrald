package query

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"

	"io"
)

// RouteInFlight routes a query through the provided Router. It returns a raw connection if query was successfully routed
// to the target and accepted, otherwise it returns an error.
// Errors: ErrRouteNotFound ErrRejected ...
func RouteInFlight(ctx *astral.Context, r astral.Router, q *astral.InFlightQuery) (astral.Conn, error) {
	ctx, cancel := ctx.WithTimeout(maxQueryTimeout)
	defer cancel()

	pipeReader, pipeWriter := io.Pipe()

	target, err := r.RouteQuery(ctx, q, pipeWriter)
	if err != nil {
		return nil, err
	}

	return NewConn(q.Caller, q.Target, target, pipeReader, true), err
}

// Route routes a Query and wraps the resulting connection in a channel.Channel
// for structured framed I/O, unlike RouteInFlight which returns a raw Conn.
func Route(ctx *astral.Context, r astral.Router, q *astral.Query) (*channel.Channel, error) {
	conn, err := RouteInFlight(ctx, r, astral.Launch(q))
	if err != nil {
		return nil, err
	}

	return channel.New(conn), nil
}

// Accept accepts the query and runs the handler in a new goroutine.
func Accept(query *astral.InFlightQuery, src io.WriteCloser, handler func(astral.Conn)) (io.WriteCloser, error) {
	pipeReader, pipeWriter := io.Pipe()

	go handler(NewConn(query.Target, query.Caller, src, pipeReader, false))

	return pipeWriter, nil
}

// Reject returns nil and an ErrRejected with the DefaultRejectCode.
func Reject() (io.WriteCloser, error) {
	return RejectWithCode(astral.DefaultRejectCode)
}

// RejectWithCode returns nil and an ErrRejected with the given code.
// The code must not be 0.
func RejectWithCode(code uint8) (io.WriteCloser, error) {
	if code == 0 {
		panic("code cannot be 0")
	}
	return nil, &astral.ErrRejected{Code: code}
}

// RouteNotFound returns nil and an ErrRouteNotFound.
func RouteNotFound() (io.WriteCloser, error) {
	return nil, astral.NewErrRouteNotFound()
}
