package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func serveObjectsAction(actor *astral.Identity, role astral.String8) *auth.ServeObjectsAction {
	return &auth.ServeObjectsAction{Action: auth.NewAction(actor), Role: role}
}

// TestServeObjectsAllowsTheUserToIndex covers the one default holder: the user,
// for the indexer role.
func TestServeObjectsAllowsTheUserToIndex(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	if !mod.AuthorizeServeObjects(nil, serveObjectsAction(userID, auth.RoleIndexer)) {
		t.Fatal("the user is the default holder of the indexer role and must be allowed")
	}
}

// TestServeObjectsRefusesTheUserOtherRoles keeps the default to indexing: the
// user holds no query role without a grant or a contract.
func TestServeObjectsRefusesTheUserOtherRoles(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder, auth.RoleSearcher, ""} {
		if mod.AuthorizeServeObjects(nil, serveObjectsAction(userID, role)) {
			t.Fatalf("the user must not hold role %q by default", role)
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

// TestServeObjectsRefusesIndexingOnUnclaimedNode covers a node with no user:
// nobody holds the indexer role by default, the node included.
func TestServeObjectsRefusesIndexingOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	for name, actor := range map[string]*astral.Identity{
		"this node": nodeID,
		"zero":      {},
	} {
		if mod.AuthorizeServeObjects(nil, serveObjectsAction(actor, auth.RoleIndexer)) {
			t.Fatalf("%s must not register an indexer on an unclaimed node", name)
		}
	}
}
