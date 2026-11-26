package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user"
)

// SyncNodesTask performs a full sync sequence with a single remote node:
// expulsions, swarm state, alias, app contracts, and asset updates.
type SyncNodesTask struct {
	remoteIdentity *astral.Identity
	mod            *Module
}

func (mod *Module) NewSyncNodesTask(remoteIdentity *astral.Identity) user.SyncNodesTask {
	return &SyncNodesTask{
		remoteIdentity: remoteIdentity,
		mod:            mod,
	}
}

func (a *SyncNodesTask) String() string {
	return "user.sync_nodes_task"
}

// Run executes the sync sequence against the remote identity in network zone order:
// expulsion push, swarm state pull, alias pull, app contract push (after a
// relay digest query), asset sync.
// Errors from individual steps are logged but do not abort the remaining steps;
// syncAssets errors are logged and swallowed — Run always returns nil.
//
// why expulsions before siblings: the remote node refuses a banned node's
// contract only once it holds the ban.
func (a *SyncNodesTask) Run(ctx *astral.Context) error {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	remoteIdentity := a.remoteIdentity
	// note: one line per run, so link flapping shows as a run count in the log.
	a.mod.log.Logv(1, "syncing with %v", remoteIdentity)

	a.mod.syncExpulsions(ctx, remoteIdentity)
	a.mod.syncSiblings(ctx, remoteIdentity)

	err := a.mod.syncAlias(ctx, remoteIdentity)
	if err != nil {
		a.mod.log.Error("error syncing alias of %v: %v", remoteIdentity, err)
	}

	a.mod.syncAppContracts(ctx, remoteIdentity)

	err = a.mod.syncAssets(ctx, remoteIdentity)
	if err != nil {
		a.mod.log.Error("error syncing assets of %v: %v", a.remoteIdentity, err)
	}

	return nil
}
