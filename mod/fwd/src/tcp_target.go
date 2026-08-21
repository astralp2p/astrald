package fwd

import (
	"io"
	_net "net"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

type TCPTarget struct {
	identity *astral.Identity
	addr     *_net.TCPAddr
}

// NewTCPTarget resolves addr to a TCPAddr at construction time; dial happens per RouteQuery call.
func NewTCPTarget(addr string, identity *astral.Identity) (*TCPTarget, error) {
	var err error
	var tcp = &TCPTarget{identity: identity}

	tcp.addr, err = _net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	return tcp, nil
}

// RouteQuery dials the TCP address and pumps conn→w in a background goroutine to bridge the two streams.
func (t *TCPTarget) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	var dialer = _net.Dialer{}

	conn, err := dialer.DialContext(ctx, "tcp", t.addr.String())
	if err != nil {
		return query.Reject()
	}

	go func() {
		io.Copy(w, conn)
		w.Close()
	}()

	return conn, nil
}

func (t *TCPTarget) String() string {
	return "tcp://" + t.addr.String()
}
