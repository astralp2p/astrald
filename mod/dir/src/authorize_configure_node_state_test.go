package dir

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// configureNodeStateOp is one row of the ConfigureNodeState surface in mod/dir:
// a dir.set_alias query that binds its required arguments.
type configureNodeStateOp struct {
	name  string
	query string
}

// configureNodeStateOps lists dir.set_alias once per mode: setting an alias and
// clearing it with an empty value.
func configureNodeStateOps(subject *astral.Identity) []configureNodeStateOp {
	return []configureNodeStateOp{
		{"dir.set_alias set", "dir.set_alias?id=" + subject.String() + "&alias=alice"},
		{"dir.set_alias clear", "dir.set_alias?id=" + subject.String() + "&alias="},
	}
}

// TestConfigureNodeStateRefusesCallerWithoutPermits is the coverage measure for
// the ConfigureNodeState action in mod/dir: setting and clearing an alias must
// ask before acting, and must reject when the answer is no.
//
// The module is a bare struct with no database. An op that reached past its
// authorization check would panic on the nil database, so "writes nothing" is
// enforced by construction as well as by the byte count.
func TestConfigureNodeStateRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range configureNodeStateOps(astral.GenerateIdentity()) {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, mod.OpSetAlias, caller, op.query, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			actions := authority.recorded()
			if len(actions) != 1 {
				t.Fatalf("%s made %d authorization calls; want exactly 1", op.name, len(actions))
			}

			checkConfigureNodeStateAction(t, op.name, actions[0], caller)
		})
	}
}

// TestConfigureNodeStateAllowsGrantedCaller covers the granted path for both
// modes: a permitted caller sets an alias and then clears it, and each change
// lands in the alias table.
func TestConfigureNodeStateAllowsGrantedCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := newConfigureNodeStateDir(t, authority)
	caller := astral.GenerateIdentity()
	subject := astral.GenerateIdentity()
	ops := configureNodeStateOps(subject)

	answerConfigureNodeState(t, mod, caller, ops[0])
	if alias, err := mod.GetAlias(subject); err != nil || alias != "alice" {
		t.Fatalf("alias after set is %q (err %v); want alice", alias, err)
	}

	answerConfigureNodeState(t, mod, caller, ops[1])
	if alias, err := mod.GetAlias(subject); err == nil {
		t.Fatalf("alias after clear is %q; want none", alias)
	}

	actions := authority.recorded()
	if len(actions) != 2 {
		t.Fatalf("set and clear made %d authorization calls; want 2", len(actions))
	}

	for i, op := range ops {
		checkConfigureNodeStateAction(t, op.name, actions[i], caller)
	}
}

// answerConfigureNodeState routes one granted query and waits for the op to
// finish answering it.
func answerConfigureNodeState(t *testing.T, mod *Module, caller *astral.Identity, op configureNodeStateOp) {
	t.Helper()

	w := newRecordingWriter()
	if err := route(t, mod.OpSetAlias, caller, op.query, w); err != nil {
		t.Fatalf("%s refused a granted caller: %v", op.name, err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish answering a granted caller", op.name)
	}

	if w.written() == 0 {
		t.Fatalf("%s wrote nothing to a granted caller", op.name)
	}
}

// checkConfigureNodeStateAction fails the test unless the recorded action is a
// ConfigureNodeState action naming the caller as the actor.
func checkConfigureNodeStateAction(t *testing.T, name string, recorded auth.ActionObject, caller *astral.Identity) {
	t.Helper()

	action, ok := recorded.(*auth.ConfigureNodeStateAction)
	if !ok {
		t.Fatalf("%s named %q; want %q", name, recorded.ObjectType(), (&auth.ConfigureNodeStateAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("%s named actor %v; want the caller %v", name, action.Actor(), caller)
	}
}

// newConfigureNodeStateDir returns a module over an in-memory alias table.
func newConfigureNodeStateDir(t *testing.T, authority *recordingAuth) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// why: every pooled connection to ":memory:" opens its own empty database.
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	if err := gdb.AutoMigrate(&dbAlias{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{Deps: Deps{Auth: authority}, db: gdb}
}
