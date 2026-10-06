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

// watchRequesterClose cancels once the requester's side ends, and not before.
func TestWatchRequesterCloseCancelsOnEOF(t *testing.T) {
	r, w := io.Pipe()
	done := make(chan struct{})
	go watchRequesterClose(r, func() { close(done) })

	select {
	case <-done:
		t.Fatal("cancelled while the requester was still connected")
	case <-time.After(50 * time.Millisecond):
	}

	_ = w.Close()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not cancelled after the requester closed")
	}
}
