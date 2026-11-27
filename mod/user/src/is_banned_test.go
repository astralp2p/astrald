package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// bannedModule is banModule on a node of its own, which IsBanned exempts.
func bannedModule(t testing.TB, issuer *astral.Identity) *Module {
	t.Helper()
	mod := banModule(t, issuer)
	nodeID := astral.GenerateIdentity()
	mod.node = &identityNode{id: nodeID}
	mod.log = log.New(nodeID)
	return mod
}

// TestIsBannedReadsTheActiveUsersBans: a subject an Expulsion in SwarmState
// names is banned, and any other identity is not.
func TestIsBannedReadsTheActiveUsersBans(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned, allowed := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	banNode(t, mod, issuer, banned)

	if !mod.IsBanned(banned) {
		t.Error("banned subject: want banned")
	}
	if mod.IsBanned(allowed) {
		t.Error("unbanned identity: want not banned")
	}
}

// TestIsBannedExemptsTheNodeAndTheUser: a stored ban on this node or on the
// user changes nothing, so an expelled node never refuses itself between
// storing its ban and leaving the swarm.
func TestIsBannedExemptsTheNodeAndTheUser(t *testing.T) {
	issuer := astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	for _, subject := range []*astral.Identity{mod.node.Identity(), issuer} {
		banNode(t, mod, issuer, subject)
	}

	if mod.IsBanned(mod.node.Identity()) {
		t.Error("this node: want not banned despite its stored ban")
	}
	if mod.IsBanned(issuer) {
		t.Error("the user: want not banned despite its stored ban")
	}
}

// TestIsBannedIsFalseWithoutAnActiveContract: with no user there is no ban to
// consult, whatever SwarmState holds.
func TestIsBannedIsFalseWithoutAnActiveContract(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	banNode(t, mod, issuer, banned)
	if !mod.IsBanned(banned) {
		t.Fatal("banned subject: want banned while the contract is active")
	}

	if err := mod.config.ActiveContract.Clear(nil); err != nil {
		t.Fatalf("clear active contract: %v", err)
	}
	if mod.IsBanned(banned) {
		t.Error("no active contract: want not banned")
	}
}
