package apphost

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// delegateNode stands in for the node routing the question to a delegate. It
// records the operation and the request, then answers with answer, or holds the
// question open when answer is nil. One node answers one question.
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

func refusingNode() *delegateNode {
	return newDelegateNode(&apphost.AppRegisterDecision{Allow: false})
}

func delegatingModule(node *delegateNode) (*Module, *policy.Delegate) {
	mod := &Module{config: defaultConfig, log: log.New(nil), node: node}
	return mod, &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
}

// registerAsk puts an empty registration to p and reports whether it admits it.
func registerAsk(ctx *astral.Context, p apphostmod.RegisterAppPolicy) bool {
	dec, err := p(ctx, &apphost.AppRegisterRequest{})
	return err == nil && bool(dec.Allow)
}

// The delegate is asked at its configured operation with the full request, and
// its decision is returned as given.
func TestDelegatedPolicyReturnsTheDelegatesDecision(t *testing.T) {
	node := newDelegateNode(&apphost.AppRegisterDecision{
		Allow:           true,
		ContractPermits: parsePermits("mod.nodes.relay_for_action"),
	})
	mod, d := delegatingModule(node)

	dec, err := mod.RegisterAppPolicyDelegated(d)(astral.NewContext(nil), &apphost.AppRegisterRequest{
		Origin:       "https://player.example",
		Name:         "Player",
		GrantPermits: parsePermits("mod.auth.serve_objects_action"),
	})
	if err != nil {
		t.Fatalf("delegated policy: %v", err)
	}

	if op := <-node.queried; op != "control.decide_app_register" {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	req := <-node.received
	if req.Origin != "https://player.example" || req.Name != "Player" || len(req.GrantPermits) != 1 {
		t.Fatalf("request reached the delegate as %+v", req)
	}
	if !dec.Allow || len(dec.GrantPermits) != 0 || len(dec.ContractPermits) != 1 {
		t.Fatalf("decision: %v, %v grant, %v contract", dec.Allow, len(dec.GrantPermits), len(dec.ContractPermits))
	}
}

// An answer that is not a decision is an error, never permission.
func TestDelegatedPolicyRefusesAnUnreadableAnswer(t *testing.T) {
	mod, d := delegatingModule(newDelegateNode(&astral.Ack{}))

	if _, err := mod.RegisterAppPolicyDelegated(d)(astral.NewContext(nil), &apphost.AppRegisterRequest{}); err == nil {
		t.Fatal("an Ack was read as a decision")
	}
}

// A delegate holding the question does not hold the registration once ctx
// ends: the call refuses and the question closes at the delegate.
func TestDelegatedPolicyEndsWithItsContext(t *testing.T) {
	node := newDelegateNode(nil)
	mod, d := delegatingModule(node)
	ctx, cancel := astral.NewContext(nil).WithCancel()

	done := make(chan bool, 1)
	go func() { done <- registerAsk(ctx, mod.RegisterAppPolicyDelegated(d)) }()

	<-node.received
	cancel()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("a cancelled question permitted the registration")
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

// boundPolicyModule binds the delegate value to leaf as LoadDependencies does.
func boundPolicyModule(t *testing.T, leaf *policyLeaf, node *delegateNode) *Module {
	t.Helper()

	mod := &Module{config: defaultConfig, log: log.New(nil), node: node}
	mod.policy.AppRegister.NoLocal = true
	if err := mod.policy.AppRegister.Bind(astral.NewContext(nil), leaf); err != nil {
		t.Fatalf("bind: %v", err)
	}
	return mod
}

// The tree value is the only state: a delegate written there is asked at once
// and after a reload, and clearing it restores accept-all.
func TestRegisterAppPolicyFollowsTheTreeValue(t *testing.T) {
	ctx := astral.NewContext(nil)
	leaf := &policyLeaf{}

	first := boundPolicyModule(t, leaf, refusingNode())
	if !registerAsk(ctx, first.GetRegisterAppPolicy()) {
		t.Fatal("with no delegate a registration was refused")
	}

	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := first.policy.AppRegister.Set(ctx, d); err != nil {
		t.Fatalf("set: %v", err)
	}
	if registerAsk(ctx, first.GetRegisterAppPolicy()) {
		t.Fatal("a delegate just set was not asked")
	}

	second := boundPolicyModule(t, leaf, refusingNode())
	if registerAsk(ctx, second.GetRegisterAppPolicy()) {
		t.Fatal("after reload the delegate's refusal was not applied")
	}

	if err := second.policy.AppRegister.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !registerAsk(ctx, boundPolicyModule(t, leaf, refusingNode()).GetRegisterAppPolicy()) {
		t.Fatal("clearing the delegate did not restore accept-all")
	}
}

// A failed save leaves the policy as it was.
func TestRegisterAppPolicyIgnoresAnUnsavedDelegate(t *testing.T) {
	ctx := astral.NewContext(nil)
	leaf := &policyLeaf{}
	mod := boundPolicyModule(t, leaf, refusingNode())
	leaf.failSet = true

	d := &policy.Delegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_app_register"}
	if err := mod.policy.AppRegister.Set(ctx, d); err == nil {
		t.Fatal("a failed save was acknowledged")
	}
	if !registerAsk(ctx, mod.GetRegisterAppPolicy()) {
		t.Fatal("an unsaved delegate was applied")
	}
}
