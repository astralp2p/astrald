package apps

import (
	"net"

	"github.com/cryptopunkscc/astral-go/api/apphost"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	libapphost "github.com/cryptopunkscc/astrald/lib/apphost"
)

// PendingQuery holds an unresolved inbound query; exactly one of Accept, Reject, RejectWithCode, Skip, or Close must be called.
type PendingQuery struct {
	conn  net.Conn
	query *astral.Query
}

// Accept accepts the query and returns a new connection
func (pending *PendingQuery) Accept() (conn *libapphost.Conn) {
	// send ack
	_ = channel.NewBinarySender(pending.conn).Send(&astral.Ack{})

	return libapphost.NewConn(pending.conn, pending.query, false)
}

// AcceptChannel accepts the query and returns a new channel
func (pending *PendingQuery) AcceptChannel(cfg ...channel.ConfigFunc) *channel.Channel {
	return channel.New(pending.Accept(), cfg...)
}

// Reject rejects the query with the default error code
func (pending *PendingQuery) Reject() (err error) {
	return pending.RejectWithCode(astral.CodeRejected)
}

// RejectWithCode rejects the query with the given code
func (pending *PendingQuery) RejectWithCode(code int) (err error) {
	defer pending.conn.Close()
	return channel.NewBinarySender(pending.conn).Send(&apphost.QueryRejectedMsg{Code: astral.Uint8(code)})
}

// Skip responds with a "route not found" error
func (pending *PendingQuery) Skip() error {
	defer pending.conn.Close()
	return channel.NewBinarySender(pending.conn).Send(&apphost.ErrorMsg{Code: apphost.ErrCodeRouteNotFound})
}

// Close closes the connection without responding
func (pending *PendingQuery) Close() error {
	return pending.conn.Close()
}

func (pending *PendingQuery) Nonce() astral.Nonce { return pending.query.Nonce }

func (pending *PendingQuery) Caller() *astral.Identity {
	return pending.query.Caller
}

func (pending *PendingQuery) Target() *astral.Identity {
	return pending.query.Target
}

func (pending *PendingQuery) Query() string {
	return pending.query.QueryString
}
