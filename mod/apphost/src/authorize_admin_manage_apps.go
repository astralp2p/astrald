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

// mayManageApps reports whether principal administers this node's apps.
//
// why: apphost.bind and apphost.cancel act on records another app owns, so an
// operator needs a way past the ownership rule to clear a stuck handler or end
// a stuck query. AdminManageApps already governs app credentials, and the user
// identity and this node hold it.
//
// note: principal is the session's authenticated identity, never q.Caller(). A
// token-less session administers nothing, so a zero principal is refused before
// the authority is asked - the core router's substitution of the node identity
// for a missing caller must not reach this.
func (mod *Module) mayManageApps(ctx *astral.Context, principal *astral.Identity) bool {
	if principal.IsZero() {
		return false
	}

	return mod.Auth.Authorize(ctx, &auth.AdminManageAppsAction{
		Action: auth.NewAction(principal),
	})
}
