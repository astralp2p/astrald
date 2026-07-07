package shell

import (
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
)

func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	if !q.Target.IsEqual(mod.node.Identity()) {
		return query.RouteNotFound()
	}

	return mod.scopes.RouteQuery(ctx, q, w)
}
