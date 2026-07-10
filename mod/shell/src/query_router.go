package shell

import (
	"io"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if !q.Target.IsEqual(mod.node.Identity()) {
		return query.RouteNotFound()
	}

	return mod.scopes.RouteQuery(ctx, q, w)
}
