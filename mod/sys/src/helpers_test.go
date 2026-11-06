package sys

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// stubUser satisfies usermod.Module; Ready, Identity, WasOwned and
// SetPolicyDelegate are exercised.
type stubUser struct {
	mu       sync.Mutex
	id       *astral.Identity
	ready    chan struct{}
	owned    bool
	policies map[string]*policy.Delegate
}

func (s *stubUser) Ready() <-chan struct{}                                        { return s.ready }
func (s *stubUser) Identity() *astral.Identity                                    { return s.id }
func (s *stubUser) LocalSwarm() []*astral.Identity                                { return nil }
func (s *stubUser) NewMaintainLinkTask(*astral.Identity) usermod.MaintainLinkTask { return nil }
func (s *stubUser) NewSyncNodesTask(*astral.Identity) usermod.SyncNodesTask       { return nil }
func (s *stubUser) PushToLocalSwarm(*astral.Context, astral.Object)               {}
func (s *stubUser) PushToSiblings(*astral.Context, astral.Object)                 {}
func (s *stubUser) Expel(*astral.Context, *astral.Identity) (*user.SignedExpulsion, error) {
	return nil, nil
}
func (s *stubUser) WasOwned() bool { return s.owned }
func (s *stubUser) SetPolicyDelegate(_ *astral.Context, name string, d *policy.Delegate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policies == nil {
		s.policies = map[string]*policy.Delegate{}
	}
	s.policies[name] = d
	return nil
}

func closedChan() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// fakeApphost provisions App identities and stores the apphost policy
// delegates in memory. A non-nil err fails ProvisionApp.
type fakeApphost struct {
	mu       sync.Mutex
	err      error
	tokens   []*apphost.AccessToken
	policies map[string]*policy.Delegate
}

func (f *fakeApphost) ProvisionApp(_ *astral.Context, _, _ []*auth.Permit) (*apphost.AccessToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	t := &apphost.AccessToken{Identity: astral.GenerateIdentity(), Token: astral.String8(astral.NewNonce().String())}
	f.tokens = append(f.tokens, t)
	return t, nil
}

func (f *fakeApphost) SetPolicyDelegate(_ *astral.Context, name string, d *policy.Delegate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.policies == nil {
		f.policies = map[string]*policy.Delegate{}
	}
	f.policies[name] = d
	return nil
}

func (f *fakeApphost) issued() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tokens)
}

// freshModule is a sys module on a fresh node with a user module stub.
func freshModule() (*Module, *fakeApphost, *stubUser) {
	apps := &fakeApphost{}
	u := &stubUser{ready: closedChan()}
	mod := &Module{log: log.New(nil), apps: apps, apphostPolicies: apps}
	mod.User = u
	return mod, apps, u
}

// recordingWriter is the caller's end of the connection.
type recordingWriter struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	closed chan struct{}
	once   sync.Once
}

func newRecordingWriter() *recordingWriter {
	return &recordingWriter{closed: make(chan struct{})}
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *recordingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

// answer waits for the op to close the connection and decodes the one object
// it answered.
func (w *recordingWriter) answer(t *testing.T) astral.Object {
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
	return obj
}

// refusal returns the text of the error_message the op answered.
func (w *recordingWriter) refusal(t *testing.T) string {
	t.Helper()
	msg, ok := w.answer(t).(*astral.ErrorMessage)
	if !ok {
		t.Fatal("the op answered no error_message")
	}
	return msg.Error()
}

// routeSending dispatches q to op fn, sends obj as its input, and returns the
// caller's end of the connection.
func routeSending(t *testing.T, fn any, q *astral.InFlightQuery, obj astral.Object) *recordingWriter {
	t.Helper()
	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	w := newRecordingWriter()
	target, err := op.RouteQuery(ctx, q, w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	// note: an op that refuses before reading closes its input, so a failed
	// send is not an error; the answer tells what happened.
	_ = channel.NewSender(target).Send(obj)
	return w
}

// localQuery is a local query from a fresh caller.
func localQuery(queryString string) *astral.InFlightQuery {
	caller := astral.GenerateIdentity()
	return astral.Launch(query.New(caller, caller, queryString, nil))
}

var _ io.WriteCloser = &recordingWriter{}
