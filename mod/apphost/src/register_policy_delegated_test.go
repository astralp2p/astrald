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

// registerAsk is the registration the tests put to a policy.
func registerAsk(ctx *astral.Context, p apphostmod.AppRegisterPolicy) bool {
	_, _, ok := p(ctx, "", "", nil, nil)
	return ok
}

// The delegate is asked at its configured operation with the full request, and
// its decision is returned as given.
func TestDelegatedPolicyReturnsTheDelegatesDecision(t *testing.T) {
	node := newDelegateNode(&apphost.AppRegisterDecision{
		Allow:           true,
		ContractPermits: parsePermits("mod.nodes.relay_for_action"),
	})
	mod, d := delegatingModule(node)

	grant, contract, ok := mod.AppRegisterPolicyDelegated(d)(
		astral.NewContext(nil), "https://player.example", "Player",
		parsePermits("mod.auth.serve_objects_action"), nil,
	)

	if op := <-node.queried; op != "control.decide_app_register" {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	req := <-node.received
	if req.Origin != "https://player.example" || req.Name != "Player" || len(req.GrantPermits) != 1 {
		t.Fatalf("request reached the delegate as %+v", req)
	}
	if !ok || len(grant) != 0 || len(contract) != 1 {
		t.Fatalf("decision: %v, %v grant, %v contract", ok, len(grant), len(contract))
	}
}

// An answer that is not a decision refuses, never permits.
func TestDelegatedPolicyRefusesAnUnreadableAnswer(t *testing.T) {
	mod, d := delegatingModule(newDelegateNode(&astral.Ack{}))

	if registerAsk(astral.NewContext(nil), mod.AppRegisterPolicyDelegated(d)) {
		t.Fatal("an Ack was read as permission")
	}
}

// A delegate holding the question does not hold the registration once ctx
// ends: the call refuses and the question closes at the delegate.
func TestDelegatedPolicyEndsWithItsContext(t *testing.T) {
	node := newDelegateNode(nil)
	mod, d := delegatingModule(node)
	ctx, cancel := astral.NewContext(nil).WithCancel()

	done := make(chan bool, 1)
	go func() { done <- registerAsk(ctx, mod.AppRegisterPolicyDelegated(d)) }()

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
	if registerAsk(ctx, second.GetAppRegisterPolicy()) {
		t.Fatal("after reload the delegate's refusal was not applied")
	}

	if err := second.SetAppRegisterPolicy(ctx, &policy.PolicyDelegate{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	third := boundPolicyModule(t, leaf, node)
	if !registerAsk(ctx, third.GetAppRegisterPolicy()) {
		t.Fatal("clearing the delegate did not restore accept-all")
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

	if !registerAsk(ctx, mod.GetAppRegisterPolicy()) {
		t.Fatal("an unsaved delegate was applied")
	}
}
