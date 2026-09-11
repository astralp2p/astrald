package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
)

// TestColdcardScanGrantsUserAndNodeOnly covers the default holders of
// coldcard.ScanAction on a claimed node: the user identity and this node's own
// identity, and no other identity.
func TestColdcardScanGrantsUserAndNodeOnly(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	userID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	// note: the tree.Value is unbound in tests, so Set seeds ActiveContract().
	err := mod.config.ActiveContract.Set(nil, &auth.SignedContract{Contract: &auth.Contract{Issuer: userID}})
	if err != nil {
		t.Fatalf("seed active contract: %v", err)
	}

	for _, tc := range []struct {
		name  string
		actor *astral.Identity
		want  bool
	}{
		{"user", userID, true},
		{"node", nodeID, true},
		{"stranger", astral.GenerateIdentity(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mod.AuthorizeColdcardScan(nil, &coldcard.ScanAction{Action: auth.NewAction(tc.actor)})
			if got != tc.want {
				t.Fatalf("AuthorizeColdcardScan(%s) = %v; want %v", tc.name, got, tc.want)
			}
		})
	}
}

// TestColdcardScanGrantsNodeOnUnclaimedNode is the property the setup ceremony
// depends on: before any user exists, an anonymous caller is promoted to this
// node's identity (core/router.go), and that identity scans.
func TestColdcardScanGrantsNodeOnUnclaimedNode(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := &Module{node: &identityNode{id: nodeID}}

	if !mod.AuthorizeColdcardScan(nil, &coldcard.ScanAction{Action: auth.NewAction(nodeID)}) {
		t.Fatal("the node must scan on an unclaimed node; the setup ceremony scans before a user exists")
	}

	stranger := astral.GenerateIdentity()
	if mod.AuthorizeColdcardScan(nil, &coldcard.ScanAction{Action: auth.NewAction(stranger)}) {
		t.Fatal("a caller that is neither the node nor the user must not scan")
	}
}
