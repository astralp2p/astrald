package messaging

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

// Expiry: a mailbox whose hosting contract lapses, the index row and the
// contract alike, and nothing renews it.

// lapsingParticipant provisions a mailbox whose hosting contract lapses two
// seconds from now.
func lapsingParticipant(t *testing.T, mod *Module) *astral.Identity {
	t.Helper()

	mod.hostingDuration = 2 * time.Second
	return hostedParticipant(t, mod)
}

// awaitLapse blocks until this node stops hosting the identity's mailbox.
func awaitLapse(t *testing.T, mod *Module, identity *astral.Identity) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for mod.hosts(identity) {
		if time.Now().After(deadline) {
			t.Fatal("the mailbox is still hosted long after its contract lapsed")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// answerOf writes obj on a routed conn and answers what the module wrote back.
func answerOf(t *testing.T, wc interface{ Write([]byte) (int, error) }, w *bufWriteCloser, obj astral.Object) astral.Object {
	t.Helper()

	if err := channel.NewSender(wc).Send(obj); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for w.String() == "" {
		if time.Now().After(deadline) {
			t.Fatal("no answer")
		}
		time.Sleep(5 * time.Millisecond)
	}

	answer, err := channel.NewReceiver(bytes.NewReader([]byte(w.String()))).Receive()
	if err != nil {
		t.Fatalf("receive answer: %v", err)
	}
	return answer
}

// A delivery and a receipt admitted before the lapse complete after it: the
// delivery is stored and acknowledged, the receipt stamps the outbox row, and a
// wait parked before the lapse answers the delivered message. Hosting was asked
// when each was admitted, and only a withdrawal stops a write — see
// whileIndexed.
func TestADeliveryAndAReceiptAdmittedBeforeTheLapseComplete(t *testing.T) {
	mod := testMessagingModule(t)
	u, peer := lapsingParticipant(t, mod), astral.GenerateIdentity()
	sent := messaging.NewMessageID()
	mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: sent, Sender: u, Recipient: peer, Content: "sent"})

	delivered := &bufWriteCloser{}
	delivery, err := mod.RouteQuery(mod.ctx, inFlight(u, messaging.MethodMessage), delivered)
	if err != nil {
		t.Fatalf("a delivery before the lapse: %v", err)
	}
	receipted := &bufWriteCloser{}
	receipt, err := mod.RouteQuery(mod.ctx, receiptFrom(peer, u), receipted)
	if err != nil {
		t.Fatalf("a receipt before the lapse: %v", err)
	}
	conn, waited := startWait(t, mod, u, "?timeout=1m")
	defer conn.Close()
	waitUntilParked(t, mod, u, 1)

	awaitLapse(t, mod, u)

	if obj := answerOf(t, delivery, delivered, &messaging.Message{ID: testID(1), Content: "late"}); !isAck(obj) {
		t.Fatalf("a delivery admitted before the lapse answered %#v after it, want an ack", obj)
	}
	if obj := answerOf(t, receipt, receipted, &messaging.Receipt{ID: sent}); !isAck(obj) {
		t.Fatalf("a receipt admitted before the lapse answered %#v after it, want an ack", obj)
	}
	if row := mustReadOwn(t, mod, u, messageRef{Box: messaging.BoxOutbox, ID: sent}); row.FetchedAt == nil {
		t.Fatal("a receipt admitted before the lapse did not stamp the outbox row")
	}

	res := onlyAnswer[*messaging.WaitResult](t, waited.objects(t))
	if res.TimedOut || len(res.Messages) != 1 || res.Messages[0].ID != testID(1) {
		t.Fatalf("a wait parked across the lapse answered %+v, want the delivered message", res)
	}
}

// A wait parked across the lapse is not cut short by it and is not refused
// after it: it ends at its granted window with what it has, here nothing.
func TestAWaitParkedAcrossTheLapseEndsAtItsBound(t *testing.T) {
	mod := testMessagingModule(t)
	u := lapsingParticipant(t, mod)

	conn, w := startWait(t, mod, u, "?timeout=4s")
	defer conn.Close()
	waitUntilParked(t, mod, u, 1)
	awaitLapse(t, mod, u)

	res := onlyAnswer[*messaging.WaitResult](t, w.objects(t))
	if !res.TimedOut || len(res.Messages) != 0 {
		t.Fatalf("the wait answered %+v, want its window closed with nothing", res)
	}
	if time.Duration(res.Granted) != 4*time.Second || res.Waited < res.Granted {
		t.Fatalf("the wait was granted %v and held %v, want 4s held in full", res.Granted, res.Waited)
	}
}

// After the lapse the mailbox is not served, and nothing is deleted: a
// delivery and a receipt are answered route not found, and the receipt stamps
// nothing; every mail method of the owner answers not a messaging participant,
// and every mail operation refuses the owner as it refuses an unhosted caller.
// The index row and the stored mail stay.
func TestAfterTheLapseTheMailboxIsNotServed(t *testing.T) {
	mod := testMessagingModule(t)
	u, peer := lapsingParticipant(t, mod), astral.GenerateIdentity()
	sent := messaging.NewMessageID()
	mustInsertOutbox(t, mod, &messaging.StoredMessage{ID: sent, Sender: u, Recipient: peer, Content: "sent"})
	awaitLapse(t, mod, u)

	_, err := mod.RouteQuery(mod.ctx, inFlight(u, messaging.MethodMessage), &bufWriteCloser{})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("a delivery after the lapse: got %v, want route not found", err)
	}
	err = routeAndWrite(t, mod, receiptFrom(peer, u), &messaging.Receipt{ID: sent})
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("a receipt after the lapse: got %v, want route not found", err)
	}
	if row := mustReadOwn(t, mod, u, messageRef{Box: messaging.BoxOutbox, ID: sent}); row.FetchedAt != nil {
		t.Fatal("a receipt after the lapse stamped the outbox row")
	}

	var tools messagingmod.Module = mod
	for name, call := range toolCalls(tools, u) {
		if err = call(); !errors.Is(err, errNotParticipant) {
			t.Fatalf("%v after the lapse: got %v, want %v", name, err, errNotParticipant)
		}
	}
	for _, op := range mailOps() {
		res := tryOp(t, op.op(mod), originQuery(u, op.name+op.args, astral.OriginLocal), op.body)
		checkUnhosted(t, op, res, true)
	}

	if _, err = mod.db.FindMailbox(u); err != nil {
		t.Fatalf("the index row went with the lapse: %v", err)
	}
	if n := countOwned(t, mod.db, u); n != 1 {
		t.Fatalf("%v stored messages after the lapse, want the one sent", n)
	}
}
