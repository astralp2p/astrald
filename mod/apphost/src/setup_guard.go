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

// admitsAnonymous reports whether a session without a token may send q. The
// session is confined to the anonymous_allowlist for the node's claim state:
// the unclaimed list while no user owns the node, the claimed list afterwards.
// An op absent from the active list is refused. webOrigin is the browser
// Origin of a WebSocket session, empty for every other session.
//
// why every such session, IPC included: core.Router names an empty caller the
// node, so an op the list admits runs with the node's authority.
// note: the session's Network zone is stripped, so no target check is needed -
// it cannot reach a peer regardless.
func (mod *Module) admitsAnonymous(ctx *astral.Context, webOrigin string, q *astral.Query) bool {
	if !mod.config.AllowAnonymous {
		return false
	}

	allowlist := mod.config.AnonymousAllowlist.Claimed
	if mod.inSetupMode(ctx) {
		// why: any page open in a local browser reaches /.ws, so only a
		// trusted origin may set up the node.
		if webOrigin != "" && !mod.isTrustedWebSource(webOrigin) {
			return false
		}
		allowlist = mod.config.AnonymousAllowlist.Unclaimed
	}

	opPath, _ := query.Parse(q.QueryString.String())
	return slices.Contains(allowlist, opPath)
}
