package astrald

/*
How to use with the default client:

	monitor := astrald.NewRouterMonitor(astrald.Default().Router)
	astrald.Default().Router = monitor

	// add inbound connections to the monitor inside your handler:
	//   monitor.Add(conn, query)

	// then fetch all active conns (thread-safe)

	for _, conn := range monitor.Conns() {}
*/

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
)

// RouterMonitor wraps a Router and tracks all active outbound connections that pass through it.
type RouterMonitor struct {
	Router
	conns sig.Map[astral.Nonce, *ConnMonitor]
}

func NewRouterMonitor(router Router) *RouterMonitor {
	return &RouterMonitor{Router: router}
}

var _ Router = &RouterMonitor{}

func (monitor *RouterMonitor) RouteQuery(context *astral.Context, query *astral.InFlightQuery) (astral.Conn, error) {
	conn, err := monitor.Router.RouteQuery(context, query)
	if err != nil {
		return nil, err
	}

	return monitor.Add(conn, query), nil
}

// Add adds a new connection to the monitor
func (monitor *RouterMonitor) Add(conn astral.Conn, query *astral.InFlightQuery) *ConnMonitor {
	connMonitor := &ConnMonitor{
		conn:         conn,
		OnClose:      func() { monitor.conns.Delete(query.Nonce) },
		OnReadError:  func(err error) { monitor.conns.Delete(query.Nonce) },
		OnWriteError: func(err error) { monitor.conns.Delete(query.Nonce) },
		query:        query,
	}

	monitor.conns.Set(query.Nonce, connMonitor)

	return connMonitor
}

// Conns returns all monitored connections
func (monitor *RouterMonitor) Conns() []*ConnMonitor {
	return monitor.conns.Values()
}

func (monitor *RouterMonitor) GuestID() *astral.Identity {
	return monitor.Router.GuestID()
}

func (monitor *RouterMonitor) HostID() *astral.Identity {
	return monitor.Router.HostID()
}
