package dir

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeSeeNodeState reports whether the query's caller may read the
// directory's alias map and filters.
//
// note: dir.alias_map, dir.filters and dir.apply_filters ask before they accept
// the query, and reject the query when the answer is no.
func (mod *Module) authorizeSeeNodeState(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.SeeNodeStateAction{
		Action: auth.NewAction(q.Caller()),
	})
}
