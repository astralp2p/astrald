package astrald

import "github.com/cryptopunkscc/astral-go/astral"

// ReadyGate Ready() returns a channel that is closed when the gate is open and replaced
// with a new open channel when the gate closes (session dropped).
type ReadyGate interface {
	Ready() <-chan struct{}
}

// GateRouter wraps a Router and blocks every outbound query until the gate signals ready.
type GateRouter struct {
	Router
	gate ReadyGate
}

var _ Router = &GateRouter{}

func NewGateRouter(r Router, g ReadyGate) *GateRouter {
	return &GateRouter{Router: r, gate: g}
}

// RouteQuery blocks until the gate is open or ctx is cancelled, then delegates to the inner router.
func (gr *GateRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery) (astral.Conn, error) {
	select {
	case <-gr.gate.Ready():
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return gr.Router.RouteQuery(ctx, q)
}
