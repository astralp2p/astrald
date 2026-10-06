package user

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// note: Crypto and Objects are nil, so an Expel that passes its guards panics.

func expulsionRowCount(t *testing.T, db *DB) int64 {
	t.Helper()

	var n int64
	if err := db.Model(&dbExpulsion{}).Count(&n).Error; err != nil {
		t.Fatalf("count expulsions: %v", err)
	}
	return n
}

func TestExpelRequiresAnActiveContract(t *testing.T) {
	mod := &Module{db: testDB(t)}

	_, err := mod.Expel(astral.NewContext(nil), astral.GenerateIdentity())
	if !errors.Is(err, user.ErrNoActiveContract) {
		t.Fatalf("Expel err = %v, want %v", err, user.ErrNoActiveContract)
	}
	if n := expulsionRowCount(t, mod.db); n != 0 {
		t.Fatalf("expulsion rows = %d, want 0", n)
	}
}

func TestExpelRefusesTheSwarmUser(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := banModule(t, userID)

	_, err := mod.Expel(astral.NewContext(nil), userID)
	if err == nil || err.Error() != "cannot expel the swarm user" {
		t.Fatalf("Expel err = %v, want %q", err, "cannot expel the swarm user")
	}
	if n := expulsionRowCount(t, mod.db); n != 0 {
		t.Fatalf("expulsion rows = %d, want 0", n)
	}
}
