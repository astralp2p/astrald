package tree

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// configureNodeStateOp is one row of the ConfigureNodeState surface in mod/tree:
// the op and a query that binds its required arguments.
type configureNodeStateOp struct {
	name  string
	op    func(*Module) any
	query string
}

// configureNodeStateOps lists every tree op that changes state, once per write
// mode: single-value and batch sets, plain and recursive deletes.
func configureNodeStateOps(target *astral.Identity) []configureNodeStateOp {
	set := func(m *Module) any { return m.OpSet }
	del := func(m *Module) any { return m.OpDelete }

	return []configureNodeStateOp{
		{"tree.set single value", set, "tree.set?path=/cfg/key&type=string8&value=hello"},
		{"tree.set batch", set, "tree.set?path=/cfg/key"},
		{"tree.delete", del, "tree.delete?path=/cfg/key"},
		{"tree.delete recursive", del, "tree.delete?path=/cfg&recursive=true"},
		{"tree.mount_remote", func(m *Module) any { return m.OpMountRemote }, "tree.mount_remote?path=/remote/peer&target=" + target.String()},
		{"tree.unmount", func(m *Module) any { return m.OpUnmount }, "tree.unmount?path=/remote/peer"},
	}
}

// TestConfigureNodeStateRefusesCallerWithoutPermits is the coverage measure for
// the ConfigureNodeState action in mod/tree: every op that changes the tree must
// ask before it acts, and must reject when the answer is no.
//
// The module is a bare struct — no mounts, no database, no directory. An op that
// reached past its authorization check would panic on a nil field, so "changes
// nothing" is enforced by construction as well as by the byte count.
func TestConfigureNodeStateRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range configureNodeStateOps(astral.GenerateIdentity()) {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.query, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			checkConfigureNodeStateAsked(t, op.name, authority, caller)
		})
	}
}

// TestConfigureNodeStateAllowsGrantedCaller covers the granted path once: the
// same guard must let a permitted caller through, and the write must land.
// Without it the refusal table above would still pass on an op that rejects
// unconditionally.
func TestConfigureNodeStateAllowsGrantedCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := newConfigureNodeStateTree(t, authority)
	caller := astral.GenerateIdentity()
	w := newRecordingWriter()

	if err := route(t, mod.OpSet, caller, "tree.set?path=/cfg/key&type=string8&value=hello", w); err != nil {
		t.Fatalf("granted caller was refused: %v", err)
	}

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("tree.set did not finish answering a granted caller")
	}

	if w.written() == 0 {
		t.Fatal("tree.set wrote nothing to a granted caller")
	}

	checkConfigureNodeStateAsked(t, "tree.set", authority, caller)

	got, err := mod.Get(astral.NewContext(nil), "/cfg/key")
	if err != nil {
		t.Fatalf("read back /cfg/key: %v", err)
	}

	if s, ok := got.(*astral.String8); !ok || string(*s) != "hello" {
		t.Fatalf("tree holds %v at /cfg/key; want string8 hello", got)
	}
}

// checkConfigureNodeStateAsked fails the test unless the op made exactly one
// authorization call, for ConfigureNodeState, naming the caller as the actor.
func checkConfigureNodeStateAsked(t *testing.T, name string, authority *recordingAuth, caller *astral.Identity) {
	t.Helper()

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("%s made %d authorization calls; want exactly 1", name, len(actions))
	}

	action, ok := actions[0].(*auth.ConfigureNodeStateAction)
	if !ok {
		t.Fatalf("%s named %q; want %q", name, actions[0].ObjectType(), (&auth.ConfigureNodeStateAction{}).ObjectType())
	}

	if !action.Actor().IsEqual(caller) {
		t.Fatalf("%s named actor %v; want the caller %v", name, action.Actor(), caller)
	}
}

// newConfigureNodeStateTree returns a module over an in-memory tree database,
// with the database root mounted at "/" as the loader mounts it.
func newConfigureNodeStateTree(t *testing.T, authority *recordingAuth) *Module {
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

	mod := &Module{
		Deps:      Deps{Auth: authority},
		db:        &DB{DB: gdb},
		nodeValue: map[int]*sig.Queue[astral.Object]{},
	}

	if err := mod.db.AutoMigrate(&dbNode{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mod.mounts.Set("/", &Node{mod: mod})

	return mod
}
