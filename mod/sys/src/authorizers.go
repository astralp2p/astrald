package sys

import (
	"github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeConfigureNodeState grants changes to this node's tree, its mounts
// and its alias table to the appointed manager, in every node state.
//
// why the manager: it answers the policies it was appointed with, and a
// policy is a tree value; see .ai/system/protocols/sys/README.md#manager.
// why after the claim too: a claim keeps the manager, and the manager keeps
// changing the policies it answers.
func (mod *Module) AuthorizeConfigureNodeState(_ *astral.Context, a *auth.ConfigureNodeStateAction) bool {
	return mod.isManager(a.Actor())
}

// AuthorizeVFSManage grants the administration of every VFS of this node to
// the appointed manager, in every node state.
//
// why the manager: it lists every VFS and sets its limits and evaluator for
// the node it manages; see .ai/system/protocols/sys/README.md#manager.
func (mod *Module) AuthorizeVFSManage(_ *astral.Context, a *vfsapi.ManageAction) bool {
	return mod.isManager(a.Actor())
}

// AuthorizeVFSRead grants reading every VFS of this node to the appointed
// manager, in every node state. No write is granted.
//
// note: the vfs module registers its own read handler concurrently, so that
// handler may ask the VFS's evaluator about the manager before this one runs.
func (mod *Module) AuthorizeVFSRead(_ *astral.Context, a *vfsapi.ReadAction) bool {
	return mod.isManager(a.Actor())
}

// isManager reports whether actor is the non-zero appointed manager.
func (mod *Module) isManager(actor *astral.Identity) bool {
	manager := mod.Manager()
	return !manager.IsZero() && actor.IsEqual(manager)
}
