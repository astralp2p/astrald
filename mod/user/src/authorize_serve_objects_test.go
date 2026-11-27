package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func serveObjectsAction(actor *astral.Identity, role astral.String8) *auth.ServeObjectsAction {
	return &auth.ServeObjectsAction{Action: auth.NewAction(actor), Role: role}
}

// TestServeObjectsAllowsTheUserToIndex covers the one default holder, the user,
// for the indexer role.
func TestServeObjectsAllowsTheUserToIndex(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	if !mod.AuthorizeServeObjects(nil, serveObjectsAction(userID, auth.RoleIndexer)) {
		t.Fatal("the user is the default holder of the indexer role and must be allowed")
	}
}

// TestServeObjectsAllowsTheUserEveryRole: the user holds the describer,
// finder, searcher and indexer roles by default.
func TestServeObjectsAllowsTheUserEveryRole(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder, auth.RoleSearcher, auth.RoleIndexer} {
		if !mod.AuthorizeServeObjects(nil, serveObjectsAction(userID, role)) {
			t.Fatalf("the user must hold role %q by default", role)
		}
	}
}

// TestServeObjectsRefusesEveryoneElseToIndex is the property indexer
// registration depends on: this node, a stranger and a zero actor are refused.
// A local caller carrying no identity arrives as this node (core/router.go).
func TestServeObjectsRefusesEveryoneElseToIndex(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := claimedModule(t, astral.GenerateIdentity())
	mod.node = &identityNode{id: nodeID}

	for name, actor := range map[string]*astral.Identity{
		"this node": nodeID,
		"stranger":  astral.GenerateIdentity(),
		"zero":      {},
		"nil":       nil,
	} {
		if mod.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleIndexer)) {
			t.Fatalf("%s must not register an indexer by default", name)
		}
	}
}

// TestServeObjectsAllowsTheUnclaimedNodeToIndex covers a node with no user:
// the node acts as the user (userOrSelf), so it holds the indexer role, and a
// stranger and a zero actor do not.
func TestServeObjectsAllowsTheUnclaimedNodeToIndex(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeServeObjects(nil, serveObjectsAction(nodeID, auth.RoleIndexer)) {
		t.Fatal("an unclaimed node acts as the user and must register an indexer")
	}
	for name, actor := range map[string]*astral.Identity{
		"stranger": astral.GenerateIdentity(),
		"zero":     {},
	} {
		if mod.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleIndexer)) {
			t.Fatalf("%s must not register an indexer on an unclaimed node", name)
		}
	}
}
