package objects

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// recordingAuth answers every Authorize call with `verdict` and keeps the actions
// it was asked about.
//
// why: the embedded nil interface satisfies authmod.Module without implementing
// it. Any method other than Authorize panics, which is the assertion that an op's
// refusal path touches nothing else in the auth module.
type recordingAuth struct {
	authmod.Module
	verdict bool

	mu      sync.Mutex
	actions []auth.ActionObject
}

func (a *recordingAuth) Authorize(_ *astral.Context, action auth.ActionObject) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, action)
	return a.verdict
}

func (a *recordingAuth) recorded() []auth.ActionObject {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]auth.ActionObject(nil), a.actions...)
}

// recordingWriter is the caller's end of the connection. Bytes reaching it mean
// the op accepted the query and answered it.
type recordingWriter struct {
	mu     sync.Mutex
	n      int
	closed chan struct{}
	once   sync.Once
}

func newRecordingWriter() *recordingWriter {
	return &recordingWriter{closed: make(chan struct{})}
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.n += len(p)
	return len(p), nil
}

func (w *recordingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func (w *recordingWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// storeOp is one row of the StoreObjects surface in mod/objects: the op, a query
// that binds its required arguments, and the nouns the call is expected to
// declare.
type storeOp struct {
	name     string
	op       func(*Module) any
	args     string
	wantRepo astral.String8
	wantType astral.String8
}

func storeObjectsOps() []storeOp {
	return []storeOp{
		{"objects.create", func(m *Module) any { return m.OpCreate }, "?repo=local", "local", ""},
		{"objects.store", func(m *Module) any { return m.OpStore }, "?repo=local", "local", ""},
		{"objects.push", func(m *Module) any { return m.OpPush }, "", "", ""},
		{"objects.new_mem", func(m *Module) any { return m.OpNewMem }, "?name=scratch&size=1M", "scratch", ""},
		{"objects.register_blueprint", func(m *Module) any { return m.OpRegisterBlueprint }, "", "", ""},
		{"objects.echo", func(m *Module) any { return m.OpEcho }, "", "", ""},
	}
}

// route dispatches one query to one op and returns the router's verdict once the
// op has resolved it.
func route(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) error {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	_, err = op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	return err
}

// TestStoreObjectsRefusesCallerWithoutPermits is the coverage measure for the
// StoreObjects action in mod/objects: every write op must ask before it acts,
// and must reject when the answer is no.
//
// The module is a bare struct — no repositories, no database, no logger. An op
// that reached past its authorization check would panic on a nil field, so
// "leaves node state unchanged" is enforced by construction as well as by the
// byte count.
func TestStoreObjectsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range storeObjectsOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

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

			action, ok := actions[0].(*auth.StoreObjectsAction)
			if !ok {
				t.Fatalf("%s named %q; want %q", op.name, actions[0].ObjectType(), (&auth.StoreObjectsAction{}).ObjectType())
			}

			if !action.Actor().IsEqual(caller) {
				t.Fatalf("%s named actor %v; want the caller %v", op.name, action.Actor(), caller)
			}

			if action.Repo != op.wantRepo {
				t.Fatalf("%s declared repo %q; want %q", op.name, action.Repo, op.wantRepo)
			}

			if action.Type != op.wantType {
				t.Fatalf("%s declared type %q; want %q", op.name, action.Type, op.wantType)
			}
		})
	}
}

// TestStoreObjectsAllowsGrantedCaller covers the granted path once: the same
// refusal machinery must let a permitted caller through. Without it the refusal
// table above would still pass on an op that rejects unconditionally.
//
// objects.echo is the subject because it touches no repository and no database,
// so a bare module answers it in full. The caller sends nothing, so echo relays
// nothing and ends on EOF.
func TestStoreObjectsAllowsGrantedCaller(t *testing.T) {
	authority := &recordingAuth{verdict: true}
	mod := &Module{Deps: Deps{Auth: authority}}
	w := newRecordingWriter()

	if err := route(t, mod.OpEcho, astral.GenerateIdentity(), "objects.echo", w); err != nil {
		t.Fatalf("granted caller was refused: %v", err)
	}

	if len(authority.recorded()) != 1 {
		t.Fatalf("objects.echo made %d authorization calls; want exactly 1", len(authority.recorded()))
	}
}
