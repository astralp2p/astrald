package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user"
)

// SyncNodesAction performs a full sync sequence with a single remote node:
// alias, active contract, sibling contracts, app contracts, and asset updates.
type SyncNodesAction struct {
	remoteIdentity *astral.Identity
	mod            *Module
}

func (mod *Module) NewSyncNodesTask(remoteIdentity *astral.
	Identity) user.SyncNodesAction {
	return &SyncNodesAction{
		remoteIdentity: remoteIdentity,
		mod:            mod,
	}
}

func (a *SyncNodesAction) String() string {
	return "user.sync_nodes_action"
}

// Run executes the sync sequence against the remote identity in network zone order:
// alias pull, active contract push, sibling contract push, app contract push, asset sync.
// Errors from individual steps are logged but do not abort the remaining steps;
// syncAssets errors are logged and swallowed — Run always returns nil.
func (a *SyncNodesAction) Run(ctx *astral.Context) error {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	remoteIdentity := a.remoteIdentity

	err := a.mod.syncAlias(ctx, remoteIdentity)
	if err != nil {
		a.mod.log.Error("error syncing alias of %v: %v", remoteIdentity, err)
	}

	a.mod.pushActiveContract(ctx, remoteIdentity)
	a.mod.syncSiblings(ctx, remoteIdentity)
	a.mod.syncExpulsions(ctx, remoteIdentity)
	a.mod.syncApps(ctx, remoteIdentity)

	err = a.mod.syncAssets(ctx, remoteIdentity)
	if err != nil {
		a.mod.log.Error("error syncing assets of %v: %v", a.remoteIdentity, err)
	}

	return nil
}
