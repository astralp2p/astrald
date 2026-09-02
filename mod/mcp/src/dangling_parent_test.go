package mcp

import (
	"testing"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// A reply to a message the recipient received is stored: the parent sits in
// the recipient's inbox.
func TestAReplyToAHeldInboxMessageIsStored(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	ask := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: ask, Sender: b, Recipient: a, Content: "ask"})

	reply := mcpapi.NewMessageID()
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: reply, Content: "reply", ParentID: ask}); err != nil {
		t.Fatalf("a reply to a held message must be stored: %v", err)
	}
	if held, _ := mod.db.Holds(a, reply); !held {
		t.Fatal("the reply must be stored")
	}
}

// A reply to a message the recipient sent is stored: the parent sits in the
// recipient's outbox. This is the ordinary two-party turn — the recipient
// holds the message it is being answered about because it sent it.
func TestAReplyToAHeldOutboxMessageIsStored(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	ask := mcpapi.NewMessageID()
	mustInsertOutbox(t, mod, &dbMessage{ID: ask, Sender: a, Recipient: b, Content: "ask"})

	reply := mcpapi.NewMessageID()
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: reply, Content: "reply", ParentID: ask}); err != nil {
		t.Fatalf("a reply to a message the recipient sent must be stored: %v", err)
	}
}

// An archived parent still counts as held: archiving a message does not unsee
// it, so a reply to it is stored.
func TestAReplyToAnArchivedParentIsStored(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	ask := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: ask, Sender: b, Recipient: a, Content: "ask"})
	if n, _ := mod.db.Archive(a, boxInbox, ask); n != 1 {
		t.Fatal("archive must move the parent")
	}

	reply := mcpapi.NewMessageID()
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: reply, Content: "reply", ParentID: ask}); err != nil {
		t.Fatalf("a reply to an archived-but-held parent must be stored: %v", err)
	}
}

// The refusal makes a wire cycle unstorable: the first of two mutually
// referencing messages names a parent the node does not hold and is refused,
// so the cycle never begins.
func TestAWireCycleIsRefusedAtTheFirstEdge(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	x, y := mcpapi.NewMessageID(), mcpapi.NewMessageID()

	// x answers y, which does not exist yet: refused
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: x, Content: "x", ParentID: y}); err == nil {
		t.Fatal("the first edge of a cycle must be refused")
	}
	if held, _ := mod.db.Holds(a, x); held {
		t.Fatal("x must not be stored")
	}
	// with x refused, y answering x now answers a message the node does not
	// hold either: refused too. No cycle, nothing stored.
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: y, Content: "y", ParentID: x}); err == nil {
		t.Fatal("the second edge must be refused as well")
	}
}

// A root message — one that answers nothing — is stored whatever else the
// inbox holds.
func TestARootMessageIsAlwaysStored(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	id := mcpapi.NewMessageID()
	if err := mod.storeMessage(b, a, &mcpapi.Message{ID: id, Content: "root"}); err != nil {
		t.Fatalf("a message that answers none must be stored: %v", err)
	}
}

// A redelivery of a stored reply is still one row: the parent it named is still
// held, so the existence check passes and the insert dedupes.
func TestARedeliveredReplyIsStillOneRow(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	ask := mcpapi.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: ask, Sender: b, Recipient: a, Content: "ask"})

	reply := mcpapi.NewMessageID()
	msg := &mcpapi.Message{ID: reply, Content: "reply", ParentID: ask}
	if err := mod.storeMessage(b, a, msg); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := mod.storeMessage(b, a, msg); err != nil {
		t.Fatalf("redelivery must be accepted, not refused: %v", err)
	}

	rows, err := mod.db.ListMessages(a, messageQuery{List: listInbox, From: b})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, r := range rows {
		if r.ID == reply {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("the reply is stored %v times, want 1", count)
	}
}

// The send tool refuses a reply to a message the agent does not hold, before
// any outbox row is written.
func TestSendRefusesAReplyToAMessageNotHeld(t *testing.T) {
	mod := testMessageModule(t)
	agent := astral.GenerateIdentity()
	peer := astral.GenerateIdentity()
	mod.Dir.(*stubDir).aliases["peer"] = peer

	stranger := mcpapi.NewMessageID()
	_, err := mod.sendMessage(agent, "peer", "reply", stranger)
	if err == nil {
		t.Fatal("a reply to a message the agent does not hold must be refused")
	}

	rows, err := mod.db.ListMessages(agent, messageQuery{List: listOutbox})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a refused send wrote %v outbox rows, want 0", len(rows))
	}
}
