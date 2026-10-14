package user

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// swarmDelegate stands in for the node routing a question to a delegate. It
// records the operation and the request, then answers with answer, or holds the
// question open when answer is nil.
type swarmDelegate struct {
	id      *astral.Identity
	answer  astral.Object
	queried chan string
	got     chan astral.Object
	closed  chan struct{}
}

func newSwarmDelegate(answer astral.Object) *swarmDelegate {
	return &swarmDelegate{
		id:      astral.GenerateIdentity(),
		answer:  answer,
		queried: make(chan string, 4),
		got:     make(chan astral.Object, 4),
		closed:  make(chan struct{}),
	}
}

func (n *swarmDelegate) Identity() *astral.Identity { return n.id }

func (n *swarmDelegate) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	n.queried <- string(q.QueryString)

	return query.Accept(q, w, func(conn astral.Conn) {
		defer close(n.closed)
		ch := channel.New(conn)

		obj, err := ch.Receive()
		if err != nil {
			return
		}
		n.got <- obj

		if n.answer == nil {
			_, _ = io.Copy(io.Discard, conn)
			return
		}
		_ = ch.Send(n.answer)
	})
}

func delegate() *policy.PolicyDelegate {
	return &policy.PolicyDelegate{Identity: astral.GenerateIdentity(), Operation: "control.decide_swarm"}
}

func TestSwarmJoinDelegateDecides(t *testing.T) {
	node := newSwarmDelegate(&user.SwarmJoinDecision{Allow: false})
	mod := &Module{node: node, log: log.New(nil)}
	requester := astral.GenerateIdentity()

	dec, err := mod.SwarmJoinRequestPolicyDelegated(delegate())(astral.NewContext(nil), &user.SwarmJoinRequest{Requester: requester})
	if err != nil || bool(dec.Allow) {
		t.Fatalf("decision %+v, %v; want the delegate's refusal", dec, err)
	}
	if op := <-node.queried; op != "control.decide_swarm" {
		t.Fatalf("asked %q, want the configured operation", op)
	}
	if req, ok := (<-node.got).(*user.SwarmJoinRequest); !ok || !req.Requester.IsEqual(requester) {
		t.Fatalf("the delegate did not receive the requester")
	}
}

func TestSwarmInviteDelegateDecides(t *testing.T) {
	node := newSwarmDelegate(&user.SwarmInviteDecision{Allow: true})
	mod := &Module{node: node, log: log.New(nil)}
	inviter := astral.GenerateIdentity()
	contract := &auth.Contract{Issuer: inviter, Subject: astral.GenerateIdentity()}

	dec, err := mod.SwarmInvitePolicyDelegated(delegate())(astral.NewContext(nil), &user.SwarmInviteRequest{Inviter: inviter, Contract: contract})
	if err != nil || !bool(dec.Allow) {
		t.Fatalf("decision %+v, %v; want the delegate's approval", dec, err)
	}
	req, ok := (<-node.got).(*user.SwarmInviteRequest)
	if !ok || !req.Inviter.IsEqual(inviter) || !req.Contract.Subject.IsEqual(contract.Subject) {
		t.Fatalf("the delegate did not receive the invitation")
	}
}

// An answer of the wrong type is an error, never permission: a join decision
// does not answer an invitation.
func TestSwarmDelegateRefusesAnUnreadableAnswer(t *testing.T) {
	mod := &Module{node: newSwarmDelegate(&user.SwarmJoinDecision{Allow: true}), log: log.New(nil)}

	_, err := mod.SwarmInvitePolicyDelegated(delegate())(astral.NewContext(nil), &user.SwarmInviteRequest{
		Contract: &auth.Contract{},
	})
	if err == nil {
		t.Fatal("a join decision was read as an invite decision")
	}
}

func TestSwarmDelegateEndsWhenTheRequesterLeaves(t *testing.T) {
	node := newSwarmDelegate(nil)
	mod := &Module{node: node, log: log.New(nil)}
	ctx, cancel := astral.NewContext(nil).WithCancel()

	done := make(chan error, 1)
	go func() {
		_, err := mod.SwarmJoinRequestPolicyDelegated(delegate())(ctx, &user.SwarmJoinRequest{Requester: astral.GenerateIdentity()})
		done <- err
	}()

	<-node.got
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a cancelled question returned a decision")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the delegated policy kept waiting after ctx ended")
	}
}

// swarmLeaf is a tree node holding one object.
type swarmLeaf struct {
	mu      sync.Mutex
	obj     astral.Object
	failSet bool
}

func (n *swarmLeaf) Get(_ *astral.Context, follow bool) (<-chan astral.Object, error) {
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

func (n *swarmLeaf) Set(_ *astral.Context, obj astral.Object) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.failSet {
		return errors.New("disk full")
	}
	n.obj = obj
	return nil
}

func (n *swarmLeaf) Delete(*astral.Context) error { return tree.ErrUnsupported }

func (n *swarmLeaf) Sub(*astral.Context) (map[string]tree.Node, error) { return nil, nil }

func (n *swarmLeaf) Create(*astral.Context, string) (tree.Node, error) {
	return nil, tree.ErrUnsupported
}

// boundSwarmModule restores the join policy from leaf as LoadDependencies does.
func boundSwarmModule(t *testing.T, leaf *swarmLeaf, node astral.Node) *Module {
	t.Helper()

	mod := &Module{node: node, log: log.New(nil)}
	mod.policy.SwarmJoin.NoLocal = true
	if err := mod.policy.SwarmJoin.Bind(astral.NewContext(nil), leaf); err != nil {
		t.Fatalf("bind: %v", err)
	}
	mod.joinPolicy = mod.selectJoinPolicy(mod.policy.SwarmJoin.Get())
	return mod
}

func joinAllowed(t *testing.T, mod *Module) bool {
	t.Helper()

	dec, err := mod.GetSwarmJoinRequestPolicy()(astral.NewContext(nil), &user.SwarmJoinRequest{Requester: astral.GenerateIdentity()})
	return err == nil && bool(dec.Allow)
}

// A delegate survives a reload; clearing it restores accept-all.
func TestSwarmJoinDelegateSelectsAndPersists(t *testing.T) {
	ctx := astral.NewContext(nil)
	leaf := &swarmLeaf{}
	node := newSwarmDelegate(&user.SwarmJoinDecision{Allow: false})

	if !joinAllowed(t, boundSwarmModule(t, leaf, node)) {
		t.Fatal("with no delegate a join was refused")
	}

	if err := boundSwarmModule(t, leaf, node).SetSwarmJoinRequestPolicy(ctx, delegate()); err != nil {
		t.Fatalf("set: %v", err)
	}

	second := boundSwarmModule(t, leaf, node)
	if joinAllowed(t, second) {
		t.Fatal("after reload the delegate's refusal was not applied")
	}

	if err := second.SetSwarmJoinRequestPolicy(ctx, &policy.PolicyDelegate{}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if !joinAllowed(t, boundSwarmModule(t, leaf, node)) {
		t.Fatal("clearing the delegate did not restore accept-all")
	}
}

func TestSwarmJoinDelegateUnsavedIsNotApplied(t *testing.T) {
	leaf := &swarmLeaf{}
	mod := boundSwarmModule(t, leaf, newSwarmDelegate(&user.SwarmJoinDecision{Allow: false}))
	leaf.failSet = true

	if err := mod.SetSwarmJoinRequestPolicy(astral.NewContext(nil), delegate()); err == nil {
		t.Fatal("a failed save was acknowledged")
	}
	if !joinAllowed(t, mod) {
		t.Fatal("an unsaved delegate was applied")
	}
}
