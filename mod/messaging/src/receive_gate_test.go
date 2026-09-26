package messaging

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// TestReceiveGateRefusesWhatTheAuthorityRefuses: the participant's mailbox is
// hosted here, and its own side turns the caller away. The answer is a
// rejection carrying a code and not a route miss, which is what lets the caller
// tell being refused from finding nobody.
func TestReceiveGateRefusesWhatTheAuthorityRefuses(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	participant := hostedParticipant(t, mod)

	_, err := mod.RouteQuery(mod.ctx, inFlight(participant, messaging.MethodMessage), &bufWriteCloser{})

	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("route: got %v, want a rejection", err)
	}
	if rejected.Code != messaging.RejectNotAdmitted {
		t.Fatalf("reject code: got %v, want %v", rejected.Code, messaging.RejectNotAdmitted)
	}
}

// TestReceiveGateAsksAboutTheCalledParticipant pins the order and the actors.
// Hosting is asked first, with this node as the actor. Then the receive
// question, whose actor is the called participant: auth walks the contracts the
// actor is subject to, so naming the caller would search a stranger's
// delegations for a permission this participant's side holds.
func TestReceiveGateAsksAboutTheCalledParticipant(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	participant := hostedParticipant(t, mod)

	q := inFlight(participant, messaging.MethodMessage)
	_, _ = mod.RouteQuery(mod.ctx, q, &bufWriteCloser{})

	asked := auth.questions()
	if len(asked) != 2 {
		t.Fatalf("the authority was asked %d times, want 2", len(asked))
	}

	hosting, ok := asked[0].(*messaging.HostMailboxAction)
	if !ok {
		t.Fatalf("first question: got %T, want *messaging.HostMailboxAction", asked[0])
	}
	if !hosting.Actor().IsEqual(mod.node.Identity()) || !hosting.MailboxID.IsEqual(participant) {
		t.Fatalf("hosting asked for %v by %v, want %v by the node", hosting.MailboxID, hosting.Actor(), participant)
	}

	action, ok := asked[1].(*messaging.ReceiveAction)
	if !ok {
		t.Fatalf("second question: got %T, want *messaging.ReceiveAction", asked[1])
	}
	if !action.Actor().IsEqual(participant) {
		t.Fatalf("actor: got %v, want the called participant %v", action.Actor(), participant)
	}
	if !action.FromID.IsEqual(q.Caller) {
		t.Fatalf("from: got %v, want the caller %v", action.FromID, q.Caller)
	}
}

// TestReceiveGateIsNotReachedForAnUnregisteredTarget keeps the question off a
// target this module does not answer for: it belongs to the other routers, and
// an authority has nothing to say about it.
func TestReceiveGateIsNotReachedForAnUnregisteredTarget(t *testing.T) {
	auth := &fakeAuth{allow: true}
	mod := testRouterModuleWithAuth(t, auth)

	_, err := mod.RouteQuery(mod.ctx, inFlight(astral.GenerateIdentity(), messaging.MethodMessage), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if n := len(auth.questions()); n != 0 {
		t.Fatalf("the authority was asked %d times about a target that is not a participant", n)
	}
}

// A query to a hosted mailbox that is neither a delivery nor a receipt is not
// this module's: it reads as a route miss, whatever the participant's side
// would answer, and asks the authority nothing. Hosting a mailbox claims no
// other query addressed to the same identity.
func TestAnUnrelatedQueryToAHostedMailboxIsNotClaimed(t *testing.T) {
	auth := &fakeAuth{allow: false}
	mod := testRouterModuleWithAuth(t, auth)
	participant := hostedParticipant(t, mod)

	_, err := mod.RouteQuery(mod.ctx, inFlight(participant, "chat.hello"), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("route: got %v, want route not found", err)
	}
	if n := len(auth.questions()); n != 0 {
		t.Fatalf("the authority was asked %d times about a query this module does not serve", n)
	}
}

// TestSendActionNamesTheSenderAndRecipient pins the outbound action's shape. The
// gate itself is exercised through a send, which needs a node to route with;
// what belongs here is that the question asked is the right one.
func TestSendActionNamesTheSenderAndRecipient(t *testing.T) {
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	action := &messaging.SendAction{
		Action: auth.NewAction(sender),
		ToID:   recipient,
	}

	if !action.Actor().IsEqual(sender) {
		t.Fatalf("actor: got %v, want the sending participant %v", action.Actor(), sender)
	}
	if !action.ToID.IsEqual(recipient) {
		t.Fatalf("to: got %v, want %v", action.ToID, recipient)
	}
	if action.ObjectType() == (&messaging.ReceiveAction{}).ObjectType() {
		t.Fatal("both directions report one object type; auth cannot tell them apart")
	}
}
