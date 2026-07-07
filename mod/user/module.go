package user

import (
	"github.com/cryptopunkscc/astral-go/astral"
)

const ModuleName = "user"
const DBPrefix = "users__"

const (
	OpSyncAssets        = "user.sync_assets"
	OpInfo              = "user.info"
	OpAddAsset          = "user.add_asset"
	OpRemoveAsset       = "user.remove_asset"
	OpAcceptMembership  = "user.accept_membership"
	OpAcceptContract    = "user.accept_contract"
	OpAdopt             = "user.adopt"
	OpRequestMembership = "user.request_membership"
	OpSwarmStatus       = "user.swarm_status"
	OpListSiblings      = "user.list_siblings"
	OpAssets            = "user.assets"
	OpNewNodeContract   = "user.new_node_contract"
	OpSyncWith          = "user.sync_with"
	OpExpel             = "user.expel"
	OpListExpelled      = "user.list_expelled"
)

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
	Expel(ctx *astral.Context, nodeID *astral.Identity) (*SignedExpulsion, error)
}
