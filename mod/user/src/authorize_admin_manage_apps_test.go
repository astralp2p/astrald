package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func adminManageAppsAction(actor *astral.Identity) *auth.AdminManageAppsAction {
	return &auth.AdminManageAppsAction{Action: auth.NewAction(actor)}
}

// TestAdminManageAppsGrantsUserAndNodeAlone pins the default holders on a
// claimed node: the user and this node hold the action, and nobody else does.
func TestAdminManageAppsGrantsUserAndNodeAlone(t *testing.T) {
	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: nodeID}

	if !mod.AuthorizeAdminManageApps(nil, adminManageAppsAction(userID)) {
		t.Fatal("the user must administer app credentials")
	}

	if !mod.AuthorizeAdminManageApps(nil, adminManageAppsAction(nodeID)) {
		t.Fatal("this node must administer app credentials; the CLI reaches the ops as the node")
	}

	if mod.AuthorizeAdminManageApps(nil, adminManageAppsAction(astral.GenerateIdentity())) {
		t.Fatal("an identity that is neither the user nor this node must not administer app credentials")
	}
}

// TestAdminManageAppsGrantsTheNodeOnUnclaimedNode: before a user exists, this
// node is the one holder.
func TestAdminManageAppsGrantsTheNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := claimedModule(t, nil)
	mod.node = &identityNode{id: nodeID}

	if !mod.AuthorizeAdminManageApps(nil, adminManageAppsAction(nodeID)) {
		t.Fatal("this node must administer app credentials on an unclaimed node")
	}

	if mod.AuthorizeAdminManageApps(nil, adminManageAppsAction(astral.GenerateIdentity())) {
		t.Fatal("a stranger must not administer app credentials on an unclaimed node")
	}
}
