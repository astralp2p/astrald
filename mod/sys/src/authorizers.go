package sys

import (
	"github.com/astralp2p/astral-go/api/auth"
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
	manager := mod.Manager()
	return !manager.IsZero() && a.Actor().IsEqual(manager)
}
