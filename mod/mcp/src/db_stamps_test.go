package mcp

import (
	"fmt"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

func outboxRow(t *testing.T, mod *Module, sender *astral.Identity, id mcp.MessageID) *mcp.StoredMessage {
	t.Helper()
	rows, err := mod.db.ListMessages(sender, messageQuery{List: listOutbox})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	for _, row := range rows {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("no outbox row under %v", id)
	return nil
}

func TestStampFetchedFromAdmitsOnlyTheRecipientOnce(t *testing.T) {
	mod := testMessageModule(t)
	a, b, c := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	for _, step := range []struct {
		name      string
		recipient *astral.Identity
		want      int64
	}{
		{"another recipient", c, 0},
		{"the recipient", b, 1},
		{"the recipient again", b, 0},
	} {
		n, err := mod.db.StampFetchedFrom(a, step.recipient, id)
		if err != nil {
			t.Fatalf("%s: StampFetchedFrom error = %v", step.name, err)
		}
		if n != step.want {
			t.Fatalf("%s: StampFetchedFrom stamped %d rows, want %d", step.name, n, step.want)
		}
	}

	if outboxRow(t, mod, a, id).FetchedAt == nil {
		t.Fatal("FetchedAt is nil after the recipient's receipt")
	}
}

func TestStampFetchedFromNeverStampsAnInboxRow(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	// note: (b, b) matches the inbox row on owner, id and recipient, so only the box guard refuses it.
	for _, pair := range [][2]*astral.Identity{{b, a}, {b, b}} {
		n, err := mod.db.StampFetchedFrom(pair[0], pair[1], id)
		if err != nil {
			t.Fatalf("StampFetchedFrom error = %v", err)
		}
		if n != 0 {
			t.Fatalf("StampFetchedFrom stamped %d rows on an inbox-only id, want 0", n)
		}
	}
}

func TestStampLandedWritesOnce(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if err := mod.db.StampLanded(a, id); err != nil {
		t.Fatalf("first StampLanded: %v", err)
	}
	first := outboxRow(t, mod, a, id).LandedAt
	if first == nil {
		t.Fatal("LandedAt is nil after StampLanded")
	}

	// why: the clock must move, or a rewrite would be indistinguishable from the first stamp.
	time.Sleep(2 * time.Millisecond)

	if err := mod.db.StampLanded(a, id); err != nil {
		t.Fatalf("second StampLanded: %v", err)
	}
	second := outboxRow(t, mod, a, id).LandedAt
	if second == nil || !second.Time().Equal(first.Time()) {
		t.Fatalf("LandedAt = %v after the second stamp, want the first %v", second, first)
	}
}

func TestStampFetchedWithNoOutboxRowIsNotAnError(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	held := mcp.NewMessageID()
	mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: held, Sender: a, Recipient: b, Content: "x"})

	if err := mod.db.StampFetched(a, mcp.NewMessageID()); err != nil {
		t.Fatalf("StampFetched on an unheld id = %v, want nil", err)
	}
	if got := outboxRow(t, mod, a, held).FetchedAt; got != nil {
		t.Fatalf("FetchedAt = %v on an unrelated row, want nil", got)
	}
}

func TestStampReceiptStoredStampsTheInboxRow(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

	if err := mod.db.StampReceiptStored(b, id); err != nil {
		t.Fatalf("StampReceiptStored: %v", err)
	}

	rows, _, err := mod.db.ReadMany(b, []messageRef{{Box: mcp.BoxInbox, ID: id}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("read inbox row: %v rows, err %v; want 1 row", len(rows), err)
	}
	if rows[0].ReceiptStoredAt == nil {
		t.Fatal("ReceiptStoredAt is nil after StampReceiptStored")
	}
}

func TestNoteDeliveryFailedStampsByOutcome(t *testing.T) {
	// note: wantErr "" stands for no refusal words on the row.
	for _, c := range []struct {
		name       string
		cause      error
		wantFailed bool
		wantErr    string
	}{
		{"no answer", fmt.Errorf("%w: eof", errNoAnswer), false, ""},
		{"unreachable", errUnreachable, true, ""},
		{"refused", fmt.Errorf("%w: busy", errRefused), true, "the recipient's node refused it: busy"},
		{"not admitted", errNotAdmitted, true, errNotAdmitted.Error()},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod := testMessageModule(t)
			a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
			id := mcp.NewMessageID()
			mustInsertOutbox(t, mod, &mcp.StoredMessage{ID: id, Sender: a, Recipient: b, Content: "x"})

			mod.noteDeliveryFailed(a, id, c.cause)

			row := outboxRow(t, mod, a, id)
			if got := row.FailedAt != nil; got != c.wantFailed {
				t.Errorf("FailedAt set = %v, want %v", got, c.wantFailed)
			}
			switch {
			case c.wantErr == "" && row.Err != nil:
				t.Errorf("Err = %q, want nil", *row.Err)
			case c.wantErr != "" && (row.Err == nil || string(*row.Err) != c.wantErr):
				t.Errorf("Err = %v, want %q", row.Err, c.wantErr)
			}
		})
	}
}
