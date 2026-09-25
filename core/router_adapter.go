package core

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
)

// routerAdapter is an adapter that allows lib/astrald client libraries to use an astral.Router directly
type routerAdapter struct {
	astral.Router
	guest *astral.Identity
	host  *astral.Identity
}

var _ astrald.Router = &routerAdapter{}

// NewClientAs returns a lib/astrald client that routes through node and names
// caller as the caller of every query it sends.
//
// why: a query forwarded to another node carries the identity it acts for, so
// the other node authorizes that identity's authority and not this node's.
// note: a nil caller routes as the node's own identity (Router.RouteQuery).
func NewClientAs(node astral.Node, caller *astral.Identity) *astrald.Client {
	return astrald.New(&routerAdapter{Router: node, guest: caller, host: node.Identity()})
}

func (r *routerAdapter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery) (astral.Conn, error) {
	// why: a link sends a query as a relay only when its caller differs from the
	// context's identity (mod/nodes/src/mux.go). A client acting for another caller
	// routes as this node, so the peer receives the caller and checks RelayFor.
	if r.guest != nil && !r.guest.IsEqual(r.host) {
		ctx = ctx.WithIdentity(r.host)
	}
	return query.RouteInFlight(ctx, r.Router, q)
}

func (r *routerAdapter) GuestID() *astral.Identity {
	return r.guest
}

func (r *routerAdapter) HostID() *astral.Identity {
	return r.host
}
