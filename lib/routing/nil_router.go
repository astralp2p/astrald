package routing

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
)

var _ astral.Router = &NilRouter{}

// NilRouter is a terminal router that always rejects queries; set Soft=true to
// return ErrRouteNotFound instead, allowing upstream routers to continue trying.
type NilRouter struct {
	Soft bool // return ErrRouteNotFound instead of ErrRejected
}

func (r *NilRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if r.Soft {
		return query.RouteNotFound()
	}
	return query.Reject()
}
