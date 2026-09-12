package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func configureNodeStateAction(actor *astral.Identity) *auth.ConfigureNodeStateAction {
	return &auth.ConfigureNodeStateAction{Action: auth.NewAction(actor)}
}

// TestConfigureNodeStateGrantsUserAndNodeOnly is the default rule for
// ConfigureNodeState on a claimed node: the user and this node are allowed, and
// an identity that is neither is refused.
func TestConfigureNodeStateGrantsUserAndNodeOnly(t *testing.T) {
	userID := astral.GenerateIdentity()
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	// note: the tree.Value is unbound in tests, so Set updates its local cache and
	// ActiveContract() reads it back.
	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: userID}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	cases := []struct {
		name  string
		actor *astral.Identity
		want  bool
	}{
		{"user", userID, true},
		{"node", nodeID, true},
		{"stranger", astral.GenerateIdentity(), false},
	}

	for _, c := range cases {
		if got := mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(c.actor)); got != c.want {
			t.Errorf("%s: AuthorizeConfigureNodeState = %v; want %v", c.name, got, c.want)
		}
	}
}

// TestConfigureNodeStateGrantsNodeOnUnclaimedNode covers a node with no user:
// the node itself is allowed, which is how a local caller carrying no identity
// reaches these ops (core/router.go), and a stranger is still refused.
func TestConfigureNodeStateGrantsNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(nodeID)) {
		t.Fatal("the node must change its own state on an unclaimed node")
	}

	if mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(astral.GenerateIdentity())) {
		t.Fatal("a stranger must not change the state of an unclaimed node")
	}
}
