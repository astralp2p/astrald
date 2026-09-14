package gateway

import (
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

// Fixtures shared by the per-action authorization tables in this package.

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

// route dispatches one query to one op and returns the router's verdict.
//
// note: the verdict is the router's, not the op's. An accepted op registers,
// reserves, forwards, and answers from its own goroutine, after this returns.
// A test reading those effects waits for them first: `answered` for an op that
// answers its caller, an op-specific signal for one that does not.
func route(t *testing.T, fn any, caller *astral.Identity, queryString string, w *recordingWriter) error {
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

// answered waits for an op to finish answering the caller, which it signals by
// closing the caller's writer.
//
// why: an op that hands the connection onward instead of answering never closes
// it — inbound `gateway.node_route` becomes a link — so that op waits on its own
// signal. A rejected query never reaches an op and closes nothing.
func answered(t *testing.T, w *recordingWriter) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("op accepted the query and never answered the caller")
	}
}
