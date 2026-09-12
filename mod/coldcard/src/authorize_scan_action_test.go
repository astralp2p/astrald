package coldcard

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// identityNode is an astral.Node that only answers Identity. Every other method
// panics on the embedded nil interface, so a rule that reached past the identity
// check fails loudly rather than silently.
type identityNode struct {
	astral.Node
	id *astral.Identity
}

func (n *identityNode) Identity() *astral.Identity { return n.id }

// userModule is a user module that only answers Identity, for the same reason.
type userModule struct {
	usermod.Module
	id *astral.Identity
}

func (u *userModule) Identity() *astral.Identity { return u.id }

func scanAction(actor *astral.Identity) *coldcard.ScanAction {
	return &coldcard.ScanAction{Action: auth.NewAction(actor)}
}

// TestScanActionGrantsNodeAndUserOnly covers the rule the coldcard module
// registers: this node and the swarm's user scan, and no other identity does.
func TestScanActionGrantsNodeAndUserOnly(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	userID := astral.GenerateIdentity()
	mod := &Module{
		node:         &identityNode{id: nodeID},
		OptionalDeps: OptionalDeps{User: &userModule{id: userID}},
	}

	for _, tc := range []struct {
		name  string
		actor *astral.Identity
		want  bool
	}{
		{"node", nodeID, true},
		{"user", userID, true},
		{"stranger", astral.GenerateIdentity(), false},
		{"zero", &astral.Identity{}, false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mod.AuthorizeScanAction(nil, scanAction(tc.actor)); got != tc.want {
				t.Fatalf("AuthorizeScanAction(%s) = %v; want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestScanActionGrantsNodeWithoutUserModule is the unclaimed-node case: the user
// module is optional, and the node scans as itself before any user exists.
func TestScanActionGrantsNodeWithoutUserModule(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeScanAction(nil, scanAction(nodeID)) {
		t.Fatal("the node must scan with no user module loaded; the setup ceremony scans before a user exists")
	}

	if mod.AuthorizeScanAction(nil, scanAction(astral.GenerateIdentity())) {
		t.Fatal("a caller that is not this node must not scan with no user module loaded")
	}
}

// TestScanActionRefusesUnclaimedUserIdentity guards the nil the user module
// answers before a claim: a zero actor must not match it.
func TestScanActionRefusesUnclaimedUserIdentity(t *testing.T) {
	mod := &Module{
		node:         &identityNode{id: astral.GenerateIdentity()},
		OptionalDeps: OptionalDeps{User: &userModule{}},
	}

	for _, actor := range []*astral.Identity{nil, {}} {
		if mod.AuthorizeScanAction(nil, scanAction(actor)) {
			t.Fatal("a zero actor must not match the nil identity of an unclaimed node")
		}
	}
}
