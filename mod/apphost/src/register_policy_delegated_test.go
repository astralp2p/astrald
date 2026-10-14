package apphost

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// delegateNode stands in for the node routing the question to a delegate. It
// records the operation and the request, then answers with answer, or holds the
// question open when answer is nil.
type delegateNode struct {
	id     *astral.Identity
	answer astral.Object

	received chan *apphost.AppRegisterRequest
	queried  chan string
	closed   chan struct{}
}

func newDelegateNode(answer astral.Object) *delegateNode {
	return &delegateNode{
		id:       astral.GenerateIdentity(),
		answer:   answer,
		received: make(chan *apphost.AppRegisterRequest, 1),
		queried:  make(chan string, 1),
		closed:   make(chan struct{}),
	}
}

func (n *delegateNode) Identity() *astral.Identity { return n.id }

func (n *delegateNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.queried <- string(q.QueryString)

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

		if n.answer == nil {
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		_ = ch.Send(n.answer)
	})
}

func delegatingModule(node *delegateNode) (*Module, *policy.PolicyDelegate) {
	mod := &Module{config: defaultConfig, log: log.New(nil), node: node}
	return mod, &policy.PolicyDelegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
}

// The delegate is asked at its configured operation with the full request, and
// its decision is returned as given.
func TestDelegatedPolicyReturnsTheDelegatesDecision(t *testing.T) {
	node := newDelegateNode(&apphost.AppRegisterDecision{
		Allow:           true,
		ContractPermits: parsePermits("mod.nodes.relay_for_action"),
		Authorizers:     []*auth.AuthorizerAssignment{{Action: "mod.auth.see_objects_action", Endpoint: "astral://media:auth.evaluate"}},
	})
	mod, d := delegatingModule(node)
	caller := astral.GenerateIdentity()

	dec, err := mod.AppRegisterPolicyDelegated(d)(astral.NewContext(nil), &apphost.AppRegisterRequest{
		Caller: caller, Anonymous: true, Name: "Player", GrantPermits: parsePermits("mod.auth.serve_objects_action"),
	})
	if err != nil {
		t.Fatalf("delegated: %v", err)
	}

	if op := <-node.queried; op != "control.decide_app_register" {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	req := <-node.received
	if !req.Caller.IsEqual(caller) || !bool(req.Anonymous) || req.Name != "Player" || len(req.GrantPermits) != 1 {
		t.Fatalf("request reached the delegate as %+v", req)
	}
	if !bool(dec.Allow) || len(dec.ContractPermits) != 1 || len(dec.Authorizers) != 1 || dec.Authorizers[0].Actor != nil {
		t.Fatalf("decision: %+v", dec)
	}
}

// An answer that is not a decision is an error, never permission.
func TestDelegatedPolicyRefusesAnUnreadableAnswer(t *testing.T) {
	mod, d := delegatingModule(newDelegateNode(&astral.Ack{}))

	if _, err := mod.AppRegisterPolicyDelegated(d)(astral.NewContext(nil), &apphost.AppRegisterRequest{}); err == nil {
		t.Fatal("an Ack was read as a decision")
	}
}

// A delegate holding the question does not hold the registration once ctx
// ends: the call returns an error and the question closes at the delegate.
func TestDelegatedPolicyEndsWhenTheRequesterLeaves(t *testing.T) {
	node := newDelegateNode(nil)
	mod, d := delegatingModule(node)
	ctx, cancel := astral.NewContext(nil).WithCancel()

	done := make(chan error, 1)
	go func() {
		_, err := mod.AppRegisterPolicyDelegated(d)(ctx, &apphost.AppRegisterRequest{})
		done <- err
	}()

	<-node.received
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled question returned a decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the delegated policy kept waiting after ctx ended")
	}

	select {
	case <-node.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the question stayed open at the delegate")
	}
}

// policyLeaf is a tree node holding one object.
type policyLeaf struct {
	mu      sync.Mutex
	obj     astral.Object
	failSet bool
}

func (n *policyLeaf) Get(_ *astral.Context, follow bool) (<-chan astral.Object, error) {
	n.mu.Lock()
	obj := n.obj
	n.mu.Unlock()
	if obj == nil {
		obj = &astral.Nil{}
	}
	ch := make(chan astral.Object, 1)
	ch <- obj
	if !follow {
		close(ch)
	}
	return ch, nil
}

func (n *policyLeaf) Set(_ *astral.Context, obj astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failSet {
		return errors.New("disk full")
	}
	n.obj = obj
	return nil
}

func (n *policyLeaf) Delete(*astral.Context) error { return tree.ErrUnsupported }

func (n *policyLeaf) Sub(*astral.Context) (map[string]tree.Node, error) { return nil, nil }

func (n *policyLeaf) Create(*astral.Context, string) (tree.Node, error) {
	return nil, tree.ErrUnsupported
}

func boundPolicyModule(t *testing.T, leaf *policyLeaf, node *delegateNode) *Module {
	t.Helper()

	mod := &Module{config: defaultConfig, log: log.New(nil), node: node}
	mod.policy.AppRegister.NoLocal = true
	if err := mod.policy.AppRegister.Bind(astral.NewContext(nil), leaf); err != nil {
		t.Fatalf("bind: %v", err)
	}
	mod.appRegisterPolicy = mod.selectAppRegisterPolicy(mod.policy.AppRegister.Get())
	return mod
}

// Setting a delegate selects the delegated policy and survives a reload;
// clearing it restores accept-all.
func TestAppRegisterDelegateSelectsAndPersists(t *testing.T) {
	ctx := astral.NewContext(nil)
	leaf := &policyLeaf{}
	node := newDelegateNode(&apphost.AppRegisterDecision{Allow: false})

	first := boundPolicyModule(t, leaf, node)
	d := &policy.PolicyDelegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := first.SetAppRegisterPolicy(ctx, d); err != nil {
		t.Fatalf("set: %v", err)
	}

	second := boundPolicyModule(t, leaf, node)
	dec, err := second.GetAppRegisterPolicy()(ctx, &apphost.AppRegisterRequest{})
	if err != nil || bool(dec.Allow) {
		t.Fatalf("after reload the delegate's refusal was not applied: %+v, %v", dec, err)
	}

	if err = second.SetAppRegisterPolicy(ctx, &policy.PolicyDelegate{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	third := boundPolicyModule(t, leaf, node)
	dec, err = third.GetAppRegisterPolicy()(ctx, &apphost.AppRegisterRequest{})
	if err != nil || !bool(dec.Allow) {
		t.Fatalf("clearing the delegate did not restore accept-all: %+v, %v", dec, err)
	}
}

// A failed save leaves the active policy as it was.
func TestAppRegisterDelegateUnsavedIsNotApplied(t *testing.T) {
	ctx := astral.NewContext(nil)
	leaf := &policyLeaf{}
	mod := boundPolicyModule(t, leaf, newDelegateNode(&apphost.AppRegisterDecision{Allow: false}))
	leaf.failSet = true

	d := &policy.PolicyDelegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := mod.SetAppRegisterPolicy(ctx, d); err == nil {
		t.Fatal("a failed save was acknowledged")
	}

	dec, err := mod.GetAppRegisterPolicy()(ctx, &apphost.AppRegisterRequest{})
	if err != nil || !bool(dec.Allow) {
		t.Fatalf("an unsaved delegate was applied: %+v, %v", dec, err)
	}
}
