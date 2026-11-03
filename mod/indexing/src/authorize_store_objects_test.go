package indexing

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
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

// recordingWriter is the caller's end of the connection; it keeps the bytes the
// op wrote and closes when the op ends.
type recordingWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func newRecordingWriter() *recordingWriter { return &recordingWriter{closed: make(chan struct{})} }

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *recordingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func (w *recordingWriter) written() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Len()
}

// refusal waits for the op to close the connection and returns the text of the
// error_message it answered.
func (w *recordingWriter) refusal(t *testing.T) string {
	t.Helper()
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	obj, err := channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
	if err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	msg, ok := obj.(*astral.ErrorMessage)
	if !ok {
		t.Fatalf("the op answered %v; want an error_message", obj)
	}
	return msg.Error()
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
// StoreObjects action in mod/indexing: the op that changes which repositories
// the node indexes must ask before it acts, and must reject when the answer is
// no.
//
// The module is a bare struct — no database, no tree nodes, no logger. An op
// that reached past its authorization check would panic on a nil field, so
// "leaves node state unchanged" is enforced by construction as well as by the
// byte count.
func TestStoreObjectsRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range []struct {
		name     string
		op       func(*Module) any
		args     string
		wantRepo astral.String8
	}{
		{"indexing.enable_repo", func(m *Module) any { return m.OpEnableRepo }, "?repo=local", "local"},
	} {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			if err != nil {
				t.Fatalf("%s: route: %v", op.name, err)
			}
			if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
				t.Fatalf("%s answered %q to a caller holding no permits; want %q", op.name, got, want)
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
		})
	}
}
