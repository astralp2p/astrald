package apphost

import (
	"slices"

	"github.com/cryptopunkscc/astrald/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// setupOps are the operations a web guest may run before the node has a user.
// note: contents unverified against a live setup run — a missing entry silently
// breaks onboarding.
var setupOps = []string{
	"user.info", // state detection (rejects code 2 when no user)
	"bip137sig.new_entropy",
	"bip137sig.mnemonic",
	"bip137sig.seed",
	"bip137sig.derive_key",
	"coldcard.scan",
	"crypto.public_key",
	"objects.store",
	"user.new_node_contract",
	"auth.sign_contract",
	"tree.set",
}

func isSetupOp(op string) bool {
	return slices.Contains(setupOps, op)
}

// inSetupMode reports whether the node has no active user. It waits for the user
// module to finish initializing so a query during startup on a configured node
// is not wrongly restricted; on ctx cancellation it errs toward setup mode.
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

// setupModeBlocks reports whether q must be refused because the node has no user
// yet. Only web guests are restricted, and only to ops outside the setup
// allowlist. A web guest's Network zone is stripped, so no target check is
// needed - it cannot reach a peer pre-user regardless.
func (mod *Module) setupModeBlocks(ctx *astral.Context, webOrigin string, q *astral.Query) bool {
	// only browser guests are restricted; native/IPC callers are trusted
	if webOrigin == "" {
		return false
	}
	// only while the node has no active user
	if !mod.inSetupMode(ctx) {
		return false
	}
	opPath, _ := query.Parse(q.QueryString)
	return !isSetupOp(opPath)
}
