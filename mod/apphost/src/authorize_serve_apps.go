package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeServeApps reports whether the query's caller may host on this node.
//
// apphost.register_handler asks this question and rejects the query when the
// answer is no, before it accepts the query or installs a handler.
//
// note: ServeApps authorizes adding the caller's own handler. It does not
// authorize removing another identity's handler.
func (mod *Module) authorizeServeApps(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeAppsAction{Action: auth.NewAction(q.Caller())})
}
