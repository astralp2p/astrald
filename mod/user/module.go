package user

import (
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

const ModuleName = "user"
const DBPrefix = "users__"

type Module interface {
	// Ready returns a channel that is closed once the module has applied the
	// initial active contract and is fully initialized.
	Ready() <-chan struct{}
	Identity() *astral.Identity
	// LocalSwarm returns the identities of swarm members reachable on this
	// device, excluding remote-only members.
	LocalSwarm() (list []*astral.Identity)
	NewMaintainLinkTask(target *astral.Identity) MaintainLinkTask
	NewSyncNodesTask(remoteIdentity *astral.Identity) SyncNodesAction
	// PushToLocalSwarm broadcasts obj to every local swarm member except the
	// node itself using ctx; delivery is best-effort and failures are silently ignored.
	PushToLocalSwarm(ctx *astral.Context, obj astral.Object)
	// Expel permanently bans nodeID from the swarm. Only the active contract's
	// issuer may expel and the ban is irreversible.
	Expel(ctx *astral.Context, nodeID *astral.Identity) (*user.SignedExpulsion, error)
}
