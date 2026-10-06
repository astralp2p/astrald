package user

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
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

// subjectNode stands in for the node being invited at user.accept_membership:
// it reads the contract and the issuer signature, then either answers with a
// subject signature or holds until the issuer closes the exchange.
type subjectNode struct {
	id     *astral.Identity
	answer *crypto.Signature // nil holds
	read   chan struct{}
	closed chan struct{}
}

var _ astral.Node = &subjectNode{}

func (n *subjectNode) Identity() *astral.Identity { return n.id }

func (n *subjectNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return query.Accept(q, w, func(conn astral.Conn) {
		defer close(n.closed)
		ch := channel.New(conn)
		for range 2 {
			if _, err := ch.Receive(); err != nil {
				return
			}
		}
		close(n.read)
		if n.answer != nil {
			_ = ch.Send(n.answer)
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	})
}

func newSubjectNode(answer *crypto.Signature) *subjectNode {
	return &subjectNode{id: astral.GenerateIdentity(), answer: answer, read: make(chan struct{}), closed: make(chan struct{})}
}

// The issuer side of user.accept_membership sends the contract and the issuer
// signature and returns the subject signature it reads back.
func TestAcceptMembershipAtReturnsTheSubjectSignature(t *testing.T) {
	want := &crypto.Signature{Scheme: "test", Data: []byte{1, 2, 3}}
	subject := newSubjectNode(want)
	mod := &Module{log: log.New(nil), node: subject}

	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	got, err := mod.acceptMembershipAt(ctx, subject.id, &auth.Contract{Subject: subject.id}, &crypto.Signature{Scheme: "test"})
	if err != nil {
		t.Fatalf("exchange failed: %v", err)
	}
	if got.Scheme != want.Scheme || !bytes.Equal(got.Data, want.Data) {
		t.Fatalf("got signature %+v, want %+v", got, want)
	}
}

// When the requester leaves while the subject's invite policy holds, ending
// ctx closes the exchange: the issuer side returns, and the subject sees the
// issuer leave so its own delegate's question can end.
func TestAcceptMembershipAtEndsWhenTheRequesterLeaves(t *testing.T) {
	subject := newSubjectNode(nil)
	mod := &Module{log: log.New(nil), node: subject}

	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := mod.acceptMembershipAt(ctx, subject.id, &auth.Contract{Subject: subject.id}, &crypto.Signature{Scheme: "test"})
		result <- err
	}()

	select {
	case <-subject.read:
	case <-time.After(5 * time.Second):
		t.Fatal("the subject never received the contract and signature")
	}

	cancel()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("the exchange succeeded after the requester left")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the exchange kept waiting after the requester left")
	}

	select {
	case <-subject.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the subject never saw the issuer leave")
	}
}

// watchRequester ends its context when the requester's side ends, and only
// observes: the reply direction stays open.
func TestUserWatchRequesterObservesWithoutClosingTheReply(t *testing.T) {
	r, w := io.Pipe()
	reply := &closeRecorder{closed: make(chan struct{})}
	conn := routing.NewConn(astral.GenerateIdentity(), astral.GenerateIdentity(), reply, r, false).(*routing.Conn)

	ctx, stop := watchRequester(astral.NewContext(nil), conn)
	defer stop()

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

type closeRecorder struct {
	closed chan struct{}
	once   sync.Once
}

func (c *closeRecorder) Write(p []byte) (int, error) { return len(p), nil }

func (c *closeRecorder) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}
