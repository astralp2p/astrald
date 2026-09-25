package messaging

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"gorm.io/gorm"
)

// countOwned answers how many rows the store holds for an owner, in either box
// and whatever the archive state. The generated column is what a listing scopes
// on, so it is what a deletion has to be measured against.
func countOwned(t *testing.T, db *DB, owner *astral.Identity) int64 {
	t.Helper()

	var n int64
	err := db.Model(&dbMessage{}).Where("owner = ?", owner).Count(&n).Error
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// A participant's mail goes with its row, and nobody else's does. The second
// half is the one a plausible fix gets wrong: a message the deleted participant
// sent exists twice, and the recipient's copy is the recipient's.
func TestDeletingAnIdentityTakesItsOwnMailAndNoOtherParticipantsMail(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	// what a holds: one received, one sent, and both rows of a note to itself
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "received",
	})
	mustInsertOutbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "sent",
	})
	self := messaging.NewMessageID()
	mustInsertOutbox(t, mod, &messaging.StoredMessage{
		ID: self, Sender: a, Recipient: a, Content: "to myself",
	})
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: self, Sender: a, Recipient: a, Content: "to myself",
	})

	// an archived row is still the participant's, and is still its own to lose
	archived := messaging.NewMessageID()
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: archived, Sender: b, Recipient: a, Content: "put away",
	})
	if _, err := mod.db.Archive(a, messaging.BoxInbox, archived); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// what b holds: its own copy of what a sent it, and one unrelated row
	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: a, Recipient: b, Content: "sent",
	})
	mustInsertOutbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "received",
	})

	if got := countOwned(t, mod.db, a); got != 5 {
		t.Fatalf("a holds %v rows before the delete, want 5", got)
	}
	if got := countOwned(t, mod.db, b); got != 2 {
		t.Fatalf("b holds %v rows before the delete, want 2", got)
	}

	if err := mod.db.CreateMailbox(a, &astral.ObjectID{Size: 1}, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	if err := mod.db.DeleteMailbox(a); err != nil {
		t.Fatalf("delete mailbox: %v", err)
	}

	if got := countOwned(t, mod.db, a); got != 0 {
		t.Fatalf("a still holds %v rows after the delete", got)
	}
	if got := countOwned(t, mod.db, b); got != 2 {
		t.Fatalf("b holds %v rows after a was deleted, want 2 — its mail is its own", got)
	}
}

// The two writes are one act. A participant the store does not hold is not
// deleted, and the mail delete that ran beside it is not committed either.
func TestDeletingAnIdentityThatIsNotHeldLeavesTheMailAlone(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	mustInsertInbox(t, mod, &messaging.StoredMessage{
		ID: messaging.NewMessageID(), Sender: b, Recipient: a, Content: "x",
	})

	// no index row for a, so the delete finds nothing to remove
	err := mod.db.DeleteMailbox(a)
	if err == nil {
		t.Fatal("deleting a participant the store does not hold answered no error")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete identity: %v, want record not found", err)
	}

	if got := countOwned(t, mod.db, a); got != 1 {
		t.Fatalf("the mail delete committed without the row: a holds %v rows, want 1", got)
	}
}

// A delivery accepted before a withdrawal and written after it stores nothing.
// Hosting is checked when the delivery is accepted and the row is written when
// the message arrives; a row written in between would be owned by an identity
// the index no longer names, which nothing lists and nothing deletes.
func TestADeliveryAcrossAWithdrawalStoresNothing(t *testing.T) {
	mod := testMessagingModule(t)
	u := hostedParticipant(t, mod)

	w := &bufWriteCloser{}
	wc, err := mod.RouteQuery(mod.ctx, inFlight(u, messaging.MethodMessage), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if err = mod.withdrawMailbox(u); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	if err = channel.NewSender(wc).Send(&messaging.Message{ID: testID(1), Content: "late"}); err != nil {
		t.Fatalf("send message: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for w.String() == "" {
		if time.Now().After(deadline) {
			t.Fatal("no answer to the delivery")
		}
		time.Sleep(5 * time.Millisecond)
	}

	obj, err := channel.NewReceiver(bytes.NewReader([]byte(w.String()))).Receive()
	if err != nil {
		t.Fatalf("receive answer: %v", err)
	}
	if isAck(obj) {
		t.Fatal("a delivery to a withdrawn mailbox was acknowledged")
	}
	if n := countOwned(t, mod.db, u); n != 0 {
		t.Fatalf("the withdrawn identity owns %v rows, want none", n)
	}
}

// A send whose sender is withdrawn after the hosting check writes no outbox
// row. sendMessage is the part of SendMessage that runs after the check.
func TestASendAcrossAWithdrawalWritesNoOutboxRow(t *testing.T) {
	mod := testMessagingModule(t)
	a, b := hostedParticipant(t, mod), hostedParticipant(t, mod)

	if err := mod.withdrawMailbox(a); err != nil {
		t.Fatalf("withdraw: %v", err)
	}

	if _, err := mod.sendMessage(a, b.String(), "late", messaging.MessageID{}); !errors.Is(err, errNotParticipant) {
		t.Fatalf("a send by a withdrawn sender: got %v, want %v", err, errNotParticipant)
	}
	if n := countOwned(t, mod.db, a) + countOwned(t, mod.db, b); n != 0 {
		t.Fatalf("the send left %v rows, want none", n)
	}
}
