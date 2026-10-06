package user

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/user"
)

func roundTrip[T astral.Object](t *testing.T, obj T) T {
	t.Helper()
	var buf bytes.Buffer
	if _, err := astral.Encode(&buf, obj); err != nil {
		t.Fatal(err)
	}
	got, err := astral.DecodeAs[T](buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The requests and decisions cross to the delegate as objects, so each must
// come back from the wire unchanged.
func TestSwarmPolicyObjectsSurviveTheWire(t *testing.T) {
	requester := astral.GenerateIdentity()
	inviter := astral.GenerateIdentity()

	join := roundTrip(t, &user.SwarmJoinRequest{Requester: requester})
	if !join.Requester.IsEqual(requester) {
		t.Fatalf("join requester changed: %v", join.Requester)
	}

	if !roundTrip(t, &user.SwarmJoinDecision{Allow: true}).Allow {
		t.Fatal("join decision lost Allow")
	}

	contract := &auth.Contract{Issuer: inviter, Subject: requester}
	invite := roundTrip(t, &user.SwarmInviteRequest{Inviter: inviter, Contract: contract})
	if !invite.Inviter.IsEqual(inviter) || invite.Contract == nil || !invite.Contract.Subject.IsEqual(requester) {
		t.Fatalf("invite request changed: %+v", invite)
	}

	if roundTrip(t, &user.SwarmInviteDecision{Allow: false}).Allow {
		t.Fatal("invite decision gained Allow")
	}
}

// With no delegate set, both swarm policies keep today's accept-all behavior.
func TestNoSwarmDelegateKeepsAcceptAll(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := astral.GenerateIdentity()

	if !mod.GetSwarmJoinRequestPolicy()(nil, id) {
		t.Fatal("join refused with no delegate set")
	}
	if !mod.GetSwarmInvitePolicy()(nil, id, &auth.Contract{Subject: id}) {
		t.Fatal("invite refused with no delegate set")
	}
}

// A delegated policy reached without a delegate refuses rather than admits.
func TestDelegatedSwarmPolicyWithoutDelegateRefuses(t *testing.T) {
	mod := &Module{log: log.New(nil)}
	id := astral.GenerateIdentity()

	if mod.SwarmJoinViaDelegate(nil, id) {
		t.Fatal("join admitted with no delegate")
	}
	if mod.SwarmInviteViaDelegate(nil, id, &auth.Contract{Subject: id}) {
		t.Fatal("invite admitted with no delegate")
	}
}

// holdingDelegate stands in for a delegate that takes the question and never
// answers. It reports when it read the request and when the node closed the
// question.
type holdingDelegate struct {
	id       *astral.Identity
	received chan astral.Object
	closed   chan struct{}
}

var _ astral.Node = &holdingDelegate{}

func (n *holdingDelegate) Identity() *astral.Identity { return n.id }

func (n *holdingDelegate) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return query.Accept(q, w, func(conn astral.Conn) {
		defer close(n.closed)
		obj, err := channel.New(conn).Receive()
		if err != nil {
			return
		}
		n.received <- obj
		_, _ = io.Copy(io.Discard, conn)
	})
}

func newHoldingDelegate() *holdingDelegate {
	return &holdingDelegate{
		id:       astral.GenerateIdentity(),
		received: make(chan astral.Object, 1),
		closed:   make(chan struct{}),
	}
}

// awaitLeave asserts that decide waits while the delegate holds, then refuses
// promptly once cancel runs, and that the question closes at the delegate.
func awaitLeave(t *testing.T, d *holdingDelegate, cancel func(), decide func() bool) {
	t.Helper()
	result := make(chan bool, 1)
	go func() { result <- decide() }()

	select {
	case <-d.received:
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
			t.Fatal("a request whose requester left was admitted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the policy kept waiting after the requester left")
	}

	select {
	case <-d.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the question stayed open at the delegate")
	}
}

// A join delegate that holds the question must not hold the request once the
// requesting node has left.
func TestSwarmJoinWaitEndsWhenTheRequesterLeaves(t *testing.T) {
	d := newHoldingDelegate()
	mod := &Module{log: log.New(nil), node: d}
	if err := mod.config.SwarmJoinDelegate.Set(nil, d.id); err != nil {
		t.Fatalf("set delegate: %v", err)
	}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	awaitLeave(t, d, cancel, func() bool {
		return mod.GetSwarmJoinRequestPolicy()(ctx, astral.GenerateIdentity())
	})
}

// An invite delegate that holds the question must not hold the invitation once
// the inviter has left.
func TestSwarmInviteWaitEndsWhenTheInviterLeaves(t *testing.T) {
	d := newHoldingDelegate()
	mod := &Module{log: log.New(nil), node: d}
	if err := mod.config.SwarmInviteDelegate.Set(nil, d.id); err != nil {
		t.Fatalf("set delegate: %v", err)
	}

	id := astral.GenerateIdentity()
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	awaitLeave(t, d, cancel, func() bool {
		return mod.GetSwarmInvitePolicy()(ctx, astral.GenerateIdentity(), &auth.Contract{Subject: id})
	})
}
