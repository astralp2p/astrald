package apps

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	libastrald "github.com/cryptopunkscc/astrald/lib/astrald"
)

// NewGateRouter wraps an inbound astral.Router and blocks all queries until gate is ready.
func NewGateRouter(router astral.Router, gate libastrald.ReadyGate) astral.Router {
	return &gateRouter{Router: router, gate: gate}
}

type gateRouter struct {
	astral.Router
	gate libastrald.ReadyGate
}

func (g *gateRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	select {
	case <-g.gate.Ready():
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return g.Router.RouteQuery(ctx, q, w)
}
