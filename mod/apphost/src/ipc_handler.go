package apphost

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/ipc"
	"github.com/astralp2p/astral-go/lib/query"
)

// IPCHandler routes queries to a registered IPC endpoint.
type IPCHandler struct {
	Identity *astral.Identity // identity of the handler
	Owner    *astral.Identity // session that registered it; nil for a token-less session
	IPCToken astral.Nonce     // token for the IPC endpoint
	Endpoint string           // IPC endpoint
}

var errEndpointUnavailable = errors.New("endpoint unavailable")

// RouteQuery dials the IPC endpoint, forwards the HandleQueryMsg, and pipes both
// directions between the caller and the local app process.
// A new connection is dialled for each query; the endpoint is expected to be a long-running daemon.
// The context bounds the dial and the wait for the app's answer, and does not bound an accepted query.
func (handler *IPCHandler) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	// why: HandleQueryMsg carries the query as a string16, and a longer string
	// fails to encode.
	if len(q.QueryString) > math.MaxUint16 {
		return query.RouteNotFound()
	}

	conn, err := ipc.DialContext(ctx, handler.Endpoint)
	// why: a dial cut short by the routing context says nothing about the endpoint, and an unavailable endpoint
	// is removed.
	switch {
	case err != nil && ctx.Err() != nil:
		return query.RouteNotFound()
	case err != nil:
		return nil, errEndpointUnavailable
	}

	err = handshake(ctx, conn, q, handler.IPCToken)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// proxy the connection traffic
	go func() {
		io.Copy(w, conn)
		w.Close()
	}()

	return conn, nil
}

// handshake sends the query to the app on conn and reads its ack or reject. When ctx ends first, conn is closed
// and handshake returns RouteNotFound.
//
// why: the app answers with no deadline of its own, so an app that reads the query and never answers would hold
// routing, and the goroutine routing it, for ever.
func handshake(ctx context.Context, conn io.ReadWriteCloser, q *astral.InFlightQuery, token astral.Nonce) error {
	closed := make(chan struct{})
	stopClose := context.AfterFunc(ctx, func() {
		defer close(closed)
		conn.Close()
	})

	err := exchange(channel.New(conn), q, token)

	// why: an accepted query outlives the routing context, so the close is stopped before success is returned.
	if !stopClose() {
		<-closed
		return astral.NewErrRouteNotFound()
	}

	return err
}

// exchange sends the HandleQueryMsg on ch and reads the app's answer.
func exchange(ch *channel.Channel, q *astral.InFlightQuery, token astral.Nonce) error {
	err := ch.Send(&apphost.HandleQueryMsg{
		IPCToken: token,
		ID:       q.Nonce,
		Caller:   q.Caller,
		Target:   q.Target,
		Query:    astral.String16(q.QueryString),
	})
	if err != nil {
		return astral.NewErrRouteNotFound()
	}

	return ch.Switch(
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
}
