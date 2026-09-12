package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func serveAppsAction(actor *astral.Identity) *auth.ServeAppsAction {
	return &auth.ServeAppsAction{Action: auth.NewAction(actor)}
}

// TestServeAppsAllowsTheUserAndTheNode covers the two default holders. A local
// caller carrying no identity is promoted to this node's identity
// (core/router.go), so the node branch is how the CLI hosts.
func TestServeAppsAllowsTheUserAndTheNode(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: nodeID}

	for _, actor := range []*astral.Identity{userID, nodeID} {
		if !mod.AuthorizeServeApps(nil, serveAppsAction(actor)) {
			t.Fatalf("%v is a default holder and must be allowed to serve apps", actor)
		}
	}
}

// TestServeAppsRefusesEveryoneElse is the property the guard depends on: an
// identity that is neither the user nor this node holds ServeApps only through
// a grant or a contract, which this handler does not consult.
func TestServeAppsRefusesEveryoneElse(t *testing.T) {
	mod := claimedModule(t, astral.GenerateIdentity())
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	for _, actor := range []*astral.Identity{astral.GenerateIdentity(), {}} {
		if mod.AuthorizeServeApps(nil, serveAppsAction(actor)) {
			t.Fatalf("%v is neither the user nor the node and must not serve apps", actor)
		}
	}
}

// TestServeAppsAllowsTheNodeOnUnclaimedNode keeps hosting open to the node
// before any user exists; a stranger is still refused.
func TestServeAppsAllowsTheNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeServeApps(nil, serveAppsAction(nodeID)) {
		t.Fatal("the node must serve apps on an unclaimed node")
	}

	if mod.AuthorizeServeApps(nil, serveAppsAction(astral.GenerateIdentity())) {
		t.Fatal("a stranger must not serve apps on an unclaimed node")
	}
}
