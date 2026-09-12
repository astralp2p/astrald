package services

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeServeApps reports whether the query's caller may host on this node.
//
// services.advertise asks this question and rejects the query when the answer
// is no, before it accepts the query or publishes an advertisement.
//
// note: ServeApps authorizes advertising the caller itself. It does not
// authorize withdrawing another identity's advertisement.
func (mod *Module) authorizeServeApps(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeAppsAction{Action: auth.NewAction(q.Caller())})
}
