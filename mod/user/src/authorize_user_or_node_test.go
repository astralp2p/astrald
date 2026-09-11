package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// TestAuthorizeUserOrNodeRefusesZeroActor pins the unclaimed-node case: the user
// identity is nil before a claim, and a nil or zero actor must not match it.
func TestAuthorizeUserOrNodeRefusesZeroActor(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	for name, actor := range map[string]*astral.Identity{
		"nil":  nil,
		"zero": {},
	} {
		if mod.authorizeUserOrNode(actor) {
			t.Fatalf("a %s actor is allowed on an unclaimed node", name)
		}
	}

	if !mod.authorizeUserOrNode(nodeID) {
		t.Fatal("this node must be allowed on an unclaimed node")
	}
}
