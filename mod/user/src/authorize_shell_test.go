package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/shell"
)

// TestAuthorizeShellAdmitsOnlyUserAndNode pins the admission rule for an
// interactive op shell. This node reaches it, because a local caller carrying
// no identity is promoted to this node's identity. Another node does not, and a
// sibling is another node here: membership alone grants no shell.
func TestAuthorizeShellAdmitsOnlyUserAndNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	action := func(actor *astral.Identity) *shell.ShellAction {
		return &shell.ShellAction{Action: auth.NewAction(actor)}
	}

	if !mod.AuthorizeShell(astral.NewContext(nil), action(nodeID)) {
		t.Fatal("this node is refused a shell on itself")
	}

	for name, actor := range map[string]*astral.Identity{
		"another node": astral.GenerateIdentity(),
		"nil":          nil,
		"zero":         {},
	} {
		if mod.AuthorizeShell(astral.NewContext(nil), action(actor)) {
			t.Fatalf("a %s actor reached the shell", name)
		}
	}
}
