package core

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/lib/astrald"
	"github.com/cryptopunkscc/astral-go/lib/query"
)

// routerAdapter is an adapter that allows lib/astrald client libraries to use an astral.Router directly
type routerAdapter struct {
	astral.Router
	identity *astral.Identity
}

var _ astrald.Router = &routerAdapter{}

func (r *routerAdapter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery) (astral.Conn, error) {
	return query.RouteInFlight(ctx, r.Router, q)
}

func (r *routerAdapter) GuestID() *astral.Identity {
	return r.identity
}

func (r *routerAdapter) HostID() *astral.Identity {
	return r.identity
}
