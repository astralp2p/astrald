package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// An archived message never reappears from wait — one of the Done clauses — and
// leaves both listings while answering under list: archive.
func TestArchivedMessagesLeaveTheListingsAndTheWait(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	n, err := mod.db.Archive(b, boxInbox, id)
	if err != nil || n != 1 {
		t.Fatalf("archive: n=%v err=%v", n, err)
	}
	if n, _ := mod.db.Archive(b, boxInbox, id); n != 0 {
		t.Fatal("archiving twice must report the second call changed nothing")
	}

	live, err := mod.listMessages(b, listRequest{List: listInbox})
	if err != nil || len(live) != 0 {
		t.Fatalf("inbox after archiving: %v rows, err %v", len(live), err)
	}

	away, err := mod.listMessages(b, listRequest{List: listArchive})
	if err != nil || len(away) != 1 {
		t.Fatalf("archive: %v rows, err %v", len(away), err)
	}

	ans, err := mod.waitMessages(context.Background(), b, waitRequest{Timeout: time.Millisecond})
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if len(ans.Rows) != 0 {
		t.Fatal("wait answered a message that was put away")
	}

	if n, _ := mod.db.Unarchive(b, boxInbox, id); n != 1 {
		t.Fatal("unarchive must put it back")
	}
	live, _ = mod.listMessages(b, listRequest{List: listInbox})
	if len(live) != 1 {
		t.Fatal("an unarchived message must return to the inbox")
	}
}

// Archiving is scoped like every other write: an id another agent holds is not
// this agent's to put away.
func TestArchiveIsScopedToItsOwner(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if n, _ := mod.db.Archive(a, boxInbox, id); n != 0 {
		t.Fatal("an agent archived a message it does not own")
	}
}

// ── the three lists, and the filters that belong to each ───────────────────

// undo runs through the same tool, so what the answer reports is whether this
// call moved the message — not whether it ended up archived, which under undo
// would name the opposite of what happened.
func TestArchiveReportsWhetherThisCallMovedIt(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &dbMessage{ID: id, Sender: b, Recipient: a, Content: "x"})

	call := func(undo bool) bool {
		t.Helper()
		_, out, err := mod.archiveTool(a)(context.Background(), nil, archiveIn{
			Box: boxInbox, ID: id.String(), Undo: undo,
		})
		if err != nil {
			t.Fatalf("archive(undo=%v): %v", undo, err)
		}
		return out.Changed
	}

	if !call(false) {
		t.Fatal("the first archive must report that it moved the message")
	}
	if call(false) {
		t.Fatal("archiving what is already away must not report a move")
	}
	if !call(true) {
		t.Fatal("undo must report that it moved the message back")
	}
	if call(true) {
		t.Fatal("undoing what is already back must not report a move")
	}

	// an id the agent does not hold answers the same false, and says nothing
	// about whether it exists elsewhere
	_, out, err := mod.archiveTool(b)(context.Background(), nil, archiveIn{
		Box: boxInbox, ID: id.String(),
	})
	if err != nil || out.Changed {
		t.Fatalf("an agent moved a message it does not hold: changed=%v err=%v", out.Changed, err)
	}
}

// ── one answer has a size, and the caller does not choose it ───────────────
