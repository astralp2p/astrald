package fwd

import (
	"io"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/tor"
)

type TorTarget struct {
	tor      tor.Module
	identity *astral.Identity
	endpoint exonet.Endpoint
}

// NewTorTarget parses addr into a Tor endpoint at construction time via the tor driver.
func NewTorTarget(drv tor.Module, addr string, identity *astral.Identity) (*TorTarget, error) {
	var err error
	var t = &TorTarget{
		identity: identity,
		tor:      drv,
	}

	t.endpoint, err = drv.Parse("tor", addr)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// RouteQuery dials the Tor endpoint and pumps conn→w in a background goroutine to bridge the two streams.
func (t *TorTarget) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	conn, err := t.tor.Dial(ctx, t.endpoint)
	if err != nil {
		return query.Reject()
	}

	go func() {
		io.Copy(w, conn)
		w.Close()
	}()

	return conn, nil
}

func (t *TorTarget) String() string {
	return "tor://" + t.endpoint.Address()
}
