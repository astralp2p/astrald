package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminManageApps reports whether the query's caller may administer the
// access tokens this node issues.
//
// Every token op asks this question and rejects the query when the answer is no,
// before it reads or changes a token and before it accepts the connection.
//
// why one action for issuing, listing and deleting: a listed token is the bearer
// credential itself, so reading the list is administration.
func (mod *Module) authorizeAdminManageApps(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{
		Action: auth.NewAction(q.Caller()),
	})
}
