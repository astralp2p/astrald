package mcp

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
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

// An agent's mail goes with its row, and nobody else's does. The second half is
// the one a plausible fix gets wrong: a message the deleted agent sent exists
// twice, and the recipient's copy is the recipient's.
func TestDeletingAnAgentTakesItsOwnMailAndNoOtherAgentsMail(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	// what a holds: one received, one sent, and both rows of a note to itself
	mustInsertInbox(t, mod, &mcp.StoredMessage{
		ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "received",
	})
	mustInsertOutbox(t, mod, &mcp.StoredMessage{
		ID: mcp.NewMessageID(), Sender: a, Recipient: b, Content: "sent",
	})
	self := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &mcp.StoredMessage{
		ID: self, Sender: a, Recipient: a, Content: "to myself",
	})
	mustInsertInbox(t, mod, &mcp.StoredMessage{
		ID: self, Sender: a, Recipient: a, Content: "to myself",
	})

	// an archived row is still the agent's, and is still its own to lose
	archived := mcp.NewMessageID()
	mustInsertInbox(t, mod, &mcp.StoredMessage{
		ID: archived, Sender: b, Recipient: a, Content: "put away",
	})
	if _, err := mod.db.Archive(a, mcp.BoxInbox, archived); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// what b holds: its own copy of what a sent it, and one unrelated row
	mustInsertInbox(t, mod, &mcp.StoredMessage{
		ID: mcp.NewMessageID(), Sender: a, Recipient: b, Content: "sent",
	})
	mustInsertOutbox(t, mod, &mcp.StoredMessage{
		ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "received",
	})

	if got := countOwned(t, mod.db, a); got != 5 {
		t.Fatalf("a holds %v rows before the delete, want 5", got)
	}
	if got := countOwned(t, mod.db, b); got != 2 {
		t.Fatalf("b holds %v rows before the delete, want 2", got)
	}

	if err := mod.db.CreateAgent(&dbAgent{Identity: a, Token: "token-a"}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := mod.db.DeleteAgent(a); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	if got := countOwned(t, mod.db, a); got != 0 {
		t.Fatalf("a still holds %v rows after the delete", got)
	}
	if got := countOwned(t, mod.db, b); got != 2 {
		t.Fatalf("b holds %v rows after a was deleted, want 2 — its mail is its own", got)
	}
}

// The two writes are one act. An agent the store does not hold is not deleted,
// and the mail delete that ran beside it is not committed either.
func TestDeletingAnAgentThatIsNotHeldLeavesTheMailAlone(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	mustInsertInbox(t, mod, &mcp.StoredMessage{
		ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "x",
	})

	// no agent row for a, so the delete finds nothing to remove
	err := mod.db.DeleteAgent(a)
	if err == nil {
		t.Fatal("deleting an agent the store does not hold answered no error")
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete agent: %v, want record not found", err)
	}

	if got := countOwned(t, mod.db, a); got != 1 {
		t.Fatalf("the mail delete committed without the row: a holds %v rows, want 1", got)
	}
}
