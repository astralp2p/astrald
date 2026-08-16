package fs

import (
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

// Fixtures for the authorization table in authorize_admin_objects_test.go.

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

// waitClosed blocks until the op has closed the caller's end.
//
// why: route returns as soon as the router resolves the query, while an accepted
// op answers on its own goroutine. Counting bytes before the close is a race.
func (w *recordingWriter) waitClosed(t *testing.T) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the op never closed the caller's end")
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
