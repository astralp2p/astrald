package apphost

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/apphost"
)

// The request and decision cross to the delegate as objects, so both must come
// back from the wire with every permit on the rail it left on.
func TestAppRegisterObjectsSurviveTheWire(t *testing.T) {
	req := &apphost.AppRegisterRequest{
		Origin:          "https://app.example",
		GrantPermits:    parsePermits("mod.auth.serve_objects_action"),
		ContractPermits: parsePermits("mod.nodes.relay_for_action,mod.auth.see_objects_action"),
		Caller:          astral.GenerateIdentity(),
		Anonymous:       true,
	}

	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, req); err != nil {
		t.Fatal(err)
	}
	got, err := astral.DecodeAs[*apphost.AppRegisterRequest](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != req.Origin || !sameActions(got.GrantPermits, req.GrantPermits) || !sameActions(got.ContractPermits, req.ContractPermits) ||
		!got.Caller.IsEqual(req.Caller) || got.Anonymous != req.Anonymous {
		t.Fatalf("request changed on the wire: %+v", got)
	}

	dec := &apphost.AppRegisterDecision{Allow: true, ContractPermits: parsePermits("mod.nodes.relay_for_action")}

	buf.Reset()
	if _, err := astral.Encode(&buf, dec); err != nil {
		t.Fatal(err)
	}
	gotDec, err := astral.DecodeAs[*apphost.AppRegisterDecision](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bool(gotDec.Allow) || len(gotDec.GrantPermits) != 0 || !sameActions(gotDec.ContractPermits, dec.ContractPermits) {
		t.Fatalf("decision changed on the wire: %+v", gotDec)
	}
}

// With no delegate set, registration keeps today's accept-all behavior.
func TestNoDelegateKeepsAcceptAll(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	grants, contracts, ok := mod.GetAppRegisterPolicy()(nil, "", nil, false, parsePermits("mod.auth.serve_objects_action"), nil)
	if !ok || len(grants) != 1 || len(contracts) != 0 {
		t.Fatalf("got grants=%v contracts=%v ok=%v, want the request back", actions(grants), actions(contracts), ok)
	}
}

// A delegated policy reached without a delegate refuses rather than admits.
func TestDelegatedPolicyWithoutDelegateRefuses(t *testing.T) {
	mod := &Module{config: defaultConfig, log: log.New(nil)}

	if _, _, ok := mod.AppRegisterViaDelegate(nil, "", nil, false, parsePermits("mod.auth.serve_objects_action"), nil); ok {
		t.Fatal("a delegated policy with no delegate admitted the registration")
	}
}

// holdingDelegate stands in for a delegate that takes the question and never
// answers, as one does while the user has not decided. It reports the request
// it read and the moment the node closed the question.
type holdingDelegate struct {
	id       *astral.Identity
	received chan *apphost.AppRegisterRequest
	closed   chan struct{}
}

var _ astral.Node = &holdingDelegate{}

func (n *holdingDelegate) Identity() *astral.Identity { return n.id }

func (n *holdingDelegate) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return query.Accept(q, w, func(conn astral.Conn) {
		defer close(n.closed)
		ch := channel.New(conn)

		obj, err := ch.Receive()
		if err != nil {
			return
		}
		if req, ok := obj.(*apphost.AppRegisterRequest); ok {
			n.received <- req
		}

		// hold until the node closes the question
		_, _ = io.Copy(io.Discard, conn)
	})
}

// A delegate that holds the question must not hold the registration once the
// requester has left: cancelling ctx ends the wait, refuses, and closes the
// question at the delegate. The request names who is registering.
func TestDelegateWaitEndsWhenTheRequesterLeaves(t *testing.T) {
	delegate := &holdingDelegate{
		id:       astral.GenerateIdentity(),
		received: make(chan *apphost.AppRegisterRequest, 1),
		closed:   make(chan struct{}),
	}
	mod := &Module{config: defaultConfig, log: log.New(nil), node: delegate}
	if err := mod.policy.AppRegisterDelegate.Set(nil, delegate.id); err != nil {
		t.Fatalf("set delegate: %v", err)
	}

	caller := astral.GenerateIdentity()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	result := make(chan bool, 1)
	go func() {
		_, _, ok := mod.GetAppRegisterPolicy()(ctx, "", caller, true, parsePermits("mod.auth.serve_objects_action"), nil)
		result <- ok
	}()

	select {
	case req := <-delegate.received:
		if !req.Caller.IsEqual(caller) || !bool(req.Anonymous) {
			t.Fatalf("delegate saw caller=%v anonymous=%v, want %v true", req.Caller, req.Anonymous, caller)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the delegate never received the request")
	}

	select {
	case <-result:
		t.Fatal("the policy decided while the delegate was still holding")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()

	select {
	case ok := <-result:
		if ok {
			t.Fatal("a registration whose requester left was admitted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the policy kept waiting after the requester left")
	}

	select {
	case <-delegate.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the question stayed open at the delegate")
	}
}

// watchRequester ends its context when the requester's side ends, and only
// observes: the reply direction stays open, so an answer can still reach a
// requester that only closed its write side.
func TestWatchRequesterObservesWithoutClosingTheReply(t *testing.T) {
	r, w := io.Pipe()
	reply := newRecordingWriter()
	conn := routing.NewConn(astral.GenerateIdentity(), astral.GenerateIdentity(), reply, r, false).(*routing.Conn)

	ctx, stop := watchRequester(astral.NewContext(nil), conn)
	defer stop()

	select {
	case <-ctx.Done():
		t.Fatal("ended while the requester was still connected")
	case <-time.After(50 * time.Millisecond):
	}

	_ = w.Close()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("did not end after the requester closed")
	}

	select {
	case <-reply.closed:
		t.Fatal("watching closed the reply direction")
	case <-time.After(50 * time.Millisecond):
	}
}

// stop ends the watch when the op returns before the requester closes, so the
// watching goroutine does not outlive the op.
func TestWatchRequesterStopEndsTheWatch(t *testing.T) {
	r, _ := io.Pipe()
	conn := routing.NewConn(astral.GenerateIdentity(), astral.GenerateIdentity(), newRecordingWriter(), r, false).(*routing.Conn)

	ctx, stop := watchRequester(astral.NewContext(nil), conn)
	stop()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not end the watch")
	}
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Fatal("stop left the read side open")
	}
}

// routeOpen dispatches one query to one op and returns the requester's write
// side, which the caller closes to end (or half-close) the requester's side.
func routeOpen(t *testing.T, fn any, caller *astral.Identity, queryString string, w io.WriteCloser) io.WriteCloser {
	t.Helper()

	op, err := routing.NewOp(fn)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	t.Cleanup(cancel)

	target, err := op.RouteQuery(ctx, astral.Launch(query.New(caller, caller, queryString, nil)), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	return target
}

func countTokens(t *testing.T, mod *Module) int {
	t.Helper()
	var tokens []dbAccessToken
	if err := mod.db.Find(&tokens).Error; err != nil {
		t.Fatalf("list tokens: %v", err)
	}
	return len(tokens)
}

// A node without a delegate keeps serving a requester that half-closes right
// after its query: it still provisions the app and the token reaches the
// requester, as before delegation existed.
func TestAcceptAllServesAHalfClosingRequester(t *testing.T) {
	mod := serveAppsRegistrar(t)
	w := newRecordingWriter()

	requester := routeOpen(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w)
	_ = requester.Close()

	awaitServeAppsClose(t, w)
	if n := countTokens(t, mod); n != 1 {
		t.Fatalf("issued %d tokens; want 1", n)
	}
	if w.written() == 0 {
		t.Fatal("the token never reached the requester")
	}
}

// With a delegate, a requester that leaves while the delegate holds the
// question ends the question at the delegate, and the node provisions nothing.
func TestDelegatedRegistrationEndsWhenTheRequesterLeaves(t *testing.T) {
	mod := serveAppsRegistrar(t)
	delegate := &holdingDelegate{
		id:       astral.GenerateIdentity(),
		received: make(chan *apphost.AppRegisterRequest, 1),
		closed:   make(chan struct{}),
	}
	mod.node = delegate
	if err := mod.policy.AppRegisterDelegate.Set(nil, delegate.id); err != nil {
		t.Fatalf("set delegate: %v", err)
	}

	w := newRecordingWriter()
	requester := routeOpen(t, mod.OpRegister, astral.GenerateIdentity(), "apphost.register", w)

	select {
	case <-delegate.received:
	case <-time.After(5 * time.Second):
		t.Fatal("the delegate never received the request")
	}

	_ = requester.Close()

	select {
	case <-delegate.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the question stayed open at the delegate after the requester left")
	}

	awaitServeAppsClose(t, w)
	if n := countTokens(t, mod); n != 0 {
		t.Fatalf("issued %d tokens to a requester that left; want 0", n)
	}
}
