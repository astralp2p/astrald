package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

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
