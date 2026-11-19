package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// grantsChange records one OnGrantsChanged call and the grant rows the DB
// held when it ran.
type grantsChange struct {
	identity *astral.Identity
	rows     int
}

// Grant, Revoke and RevokeGrant each run OnGrantsChanged exactly once with the
// identity, after the DB holds the change. A revoke that deletes nothing runs
// no callback.
func TestGrantChangesRunOnGrantsChangedOnceAfterTheWrite(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()
	var calls []grantsChange
	mod.OnGrantsChanged(func(id *astral.Identity) {
		calls = append(calls, grantsChange{identity: id, rows: len(grantIDs(t, mod, app))})
	})

	steps := []struct {
		name string
		do   func() error
		rows int // the rows the DB holds after the step
	}{
		{"grant describer", func() error { return mod.Grant(app, servePermit(auth.RoleDescriber), nil) }, 1},
		{"grant finder", func() error { return mod.Grant(app, servePermit(auth.RoleFinder), nil) }, 2},
		{"revoke one by ID", func() error { return mod.RevokeGrant(app, grantIDs(t, mod, app)[0]) }, 1},
		{"revoke the action", func() error { return mod.Revoke(app, auth.ServeObjectsAction{}.ObjectType()) }, 0},
	}
	for i, step := range steps {
		if err := step.do(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if len(calls) != i+1 {
			t.Fatalf("%s: %d callbacks in all; want %d", step.name, len(calls), i+1)
		}
		got := calls[i]
		if !got.identity.IsEqual(app) {
			t.Fatalf("%s: callback for %v; want %v", step.name, got.identity, app)
		}
		if got.rows != step.rows {
			t.Fatalf("%s: callback saw %d rows; want %d, the state after the write", step.name, got.rows, step.rows)
		}
	}

	if err := mod.Revoke(app, auth.ServeObjectsAction{}.ObjectType()); err == nil {
		t.Fatal("revoking an action the app holds no grant for succeeded")
	}
	if len(calls) != len(steps) {
		t.Fatalf("a revoke that deleted nothing ran a callback")
	}
}

// A refused grant records nothing and runs no callback.
func TestRefusedGrantRunsNoCallback(t *testing.T) {
	mod := testGrantModule(t)
	ran := 0
	mod.OnGrantsChanged(func(*astral.Identity) { ran++ })
	if err := mod.Grant(astral.GenerateIdentity(), &auth.Permit{}, nil); err == nil {
		t.Fatal("an empty permit was granted")
	}
	if ran != 0 {
		t.Fatalf("callbacks ran %d times; want 0", ran)
	}
}
