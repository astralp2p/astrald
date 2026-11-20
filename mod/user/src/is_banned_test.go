package user

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// bannedModule is banModule on a node of its own, which isBanned exempts.
func bannedModule(t *testing.T, issuer *astral.Identity) *Module {
	t.Helper()
	mod := banModule(t, issuer)
	nodeID := astral.GenerateIdentity()
	mod.node = &identityNode{id: nodeID}
	mod.log = log.New(nodeID)
	return mod
}

// TestIsBannedReadsTheActiveUsersBans: a subject the active user banned is
// banned, any other identity is not, and a ban made under another issuer
// reaches nobody.
func TestIsBannedReadsTheActiveUsersBans(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned, allowed, foreign := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	for _, signed := range []struct{ issuer, subject *astral.Identity }{
		{issuer, banned},
		{astral.GenerateIdentity(), foreign},
	} {
		if err := mod.db.StoreExpulsion(sampleSigned(signed.issuer, signed.subject)); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	mod.refreshBans()

	if !mod.isBanned(banned) {
		t.Error("banned subject: want banned")
	}
	if mod.isBanned(allowed) {
		t.Error("unbanned identity: want not banned")
	}
	if mod.isBanned(foreign) {
		t.Error("subject of another issuer's ban: want not banned")
	}
}

// TestIsBannedExemptsTheNodeAndTheUser: a stored ban on this node or on the
// user changes nothing, so an expelled node never refuses itself between
// storing its ban and leaving the swarm.
func TestIsBannedExemptsTheNodeAndTheUser(t *testing.T) {
	issuer := astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	for _, subject := range []*astral.Identity{mod.node.Identity(), issuer} {
		if err := mod.db.StoreExpulsion(sampleSigned(issuer, subject)); err != nil {
			t.Fatalf("store: %v", err)
		}
	}
	mod.refreshBans()

	if mod.isBanned(mod.node.Identity()) {
		t.Error("this node: want not banned despite its stored ban")
	}
	if mod.isBanned(issuer) {
		t.Error("the user: want not banned despite its stored ban")
	}
}

// TestIsBannedIsFalseWithoutAnActiveContract: with no user there is no ban set
// to consult, whatever the database holds and whatever was loaded before.
func TestIsBannedIsFalseWithoutAnActiveContract(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, banned)); err != nil {
		t.Fatalf("store: %v", err)
	}
	mod.refreshBans()
	if !mod.isBanned(banned) {
		t.Fatal("banned subject: want banned while the contract is active")
	}

	if err := mod.config.ActiveContract.Clear(nil); err != nil {
		t.Fatalf("clear active contract: %v", err)
	}
	if mod.isBanned(banned) {
		t.Error("no active contract: want not banned even before the set is refreshed")
	}
	mod.refreshBans()
	if mod.isBanned(banned) {
		t.Error("no active contract: want not banned after the refresh")
	}
}

// TestIsBannedLoadsTheSetOnFirstUse: before Run has loaded the set, the first
// question loads it instead of answering from nothing, so a ban stored before
// the start holds from the first authorization.
func TestIsBannedLoadsTheSetOnFirstUse(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned, allowed := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, banned)); err != nil {
		t.Fatalf("store: %v", err)
	}
	if mod.banned.Load() != nil {
		t.Fatal("the set was loaded before the first question")
	}

	if mod.isBanned(allowed) {
		t.Error("unbanned identity: want not banned")
	}
	if mod.banned.Load() == nil {
		t.Fatal("the first question did not load the set")
	}
	if !mod.isBanned(banned) {
		t.Error("banned subject: want banned")
	}
}

// TestIsBannedFailsOpenWhenTheSetCannotBeRead: a ban store that cannot be read
// leaves every identity unbanned, as AdmitLink does, and the next question
// reads again.
func TestIsBannedFailsOpenWhenTheSetCannotBeRead(t *testing.T) {
	issuer := astral.GenerateIdentity()
	banned := astral.GenerateIdentity()
	mod := bannedModule(t, issuer)
	if err := mod.db.StoreExpulsion(sampleSigned(issuer, banned)); err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := mod.db.Migrator().RenameTable(&dbExpulsion{}, "unreadable"); err != nil {
		t.Fatalf("hide the ban table: %v", err)
	}

	if mod.isBanned(banned) {
		t.Error("unreadable ban store: want not banned")
	}
	if mod.banned.Load() != nil {
		t.Error("a failed read stored a set")
	}

	if err := mod.db.Migrator().RenameTable("unreadable", &dbExpulsion{}); err != nil {
		t.Fatalf("restore the ban table: %v", err)
	}
	if !mod.isBanned(banned) {
		t.Error("readable again: want banned")
	}
}
