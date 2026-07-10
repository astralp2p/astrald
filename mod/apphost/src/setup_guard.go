package apphost

import (
	"slices"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// inSetupMode reports whether the node has no active user - i.e. is unclaimed.
// It waits for the user module to finish initializing so a query during startup
// on a claimed node is not wrongly restricted; on ctx cancellation it errs
// toward setup mode.
func (mod *Module) inSetupMode(ctx *astral.Context) bool {
	if mod.User == nil {
		return true
	}
	select {
	case <-mod.User.Ready():
		return mod.User.Identity().IsZero()
	case <-ctx.Done():
		return true
	}
}

// blocksAnonymousWeb reports whether q must be refused. Only unauthenticated
// browser guests are gated: IPC callers (empty origin) and token-authenticated
// guests are trusted with the full op surface. A gated guest is confined to the
// anonymous_web_allowlist for the node's claim state - the unclaimed list while
// no user owns the node, the claimed list afterwards. An op absent from the
// active list is refused. A web guest's Network zone is stripped, so no target
// check is needed - it cannot reach a peer regardless.
func (mod *Module) blocksAnonymousWeb(ctx *astral.Context, webOrigin string, authenticated bool, q *astral.Query) bool {
	if webOrigin == "" || authenticated {
		return false
	}

	allowlist := mod.config.AnonymousWebAllowlist.Claimed
	if mod.inSetupMode(ctx) {
		allowlist = mod.config.AnonymousWebAllowlist.Unclaimed
	}

	opPath, _ := query.Parse(q.QueryString)
	return !slices.Contains(allowlist, opPath)
}
