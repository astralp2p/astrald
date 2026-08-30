package mcp

import (
	"context"
	"testing"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// A first message is the root of its own exchange, so every message is in a
// thread and a sender learns the name of the one it started.
func TestSendMessageStartsAThread(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := sendPair(t, mod)

	_, out, err := mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "which port?",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if out.Thread != out.ID {
		t.Fatalf("thread %v, want the message's own id %v", out.Thread, out.ID)
	}

	rows, err := mod.db.ListInbox(recipient, inboxQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if rows[0].Thread.String() != out.Thread {
		t.Fatalf("stored thread %v, want %v", rows[0].Thread, out.Thread)
	}
}

// A reply names the thread it answers, and both messages carry the same label.
// The label is flat: a reply to a reply carries the root's, never its parent's.
func TestReplyCarriesTheRootThread(t *testing.T) {
	mod := testMessageModule(t)
	a, b := registeredAgent(mod), registeredAgent(mod)
	_ = mod.Dir.SetAlias(b, "b")
	_ = mod.Dir.SetAlias(a, "a")

	_, ask, err := mod.sendMessageTool(a)(context.Background(), nil, sendMessageIn{To: "b", Content: "which port?"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}

	_, reply, err := mod.sendMessageTool(b)(context.Background(), nil, sendMessageIn{
		To: "a", Content: "8626", Thread: ask.Thread,
	})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if reply.Thread != ask.Thread {
		t.Fatalf("reply thread %v, want %v", reply.Thread, ask.Thread)
	}

	_, third, err := mod.sendMessageTool(a)(context.Background(), nil, sendMessageIn{
		To: "b", Content: "thanks", Thread: reply.Thread,
	})
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if third.Thread != ask.Thread {
		t.Fatalf("a reply to a reply carries %v, want the root %v", third.Thread, ask.Thread)
	}
}

// A peer on a node that predates the field names no thread. The recipient's
// node settles it rather than storing a message in no exchange at all.
func TestArrivingWithNoThreadRootsItself(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	err := mod.storeMessage(sender, recipient, &mcpapi.Message{
		ID: testID(1), Content: "no thread named",
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	rows, err := mod.db.ListInbox(recipient, inboxQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if rows[0].Thread != testID(1) {
		t.Fatalf("thread %v, want the message's own id", rows[0].Thread)
	}
	if rows[0].Thread.IsZero() {
		t.Fatal("a stored message is in no thread")
	}
}

// Selective receive: a reader waiting on one exchange leaves every other
// message where it is, so it never claims mail it cannot give back.
func TestReadNextClaimsOnlyWhatItAsksFor(t *testing.T) {
	mod := testMessageModule(t)
	me := registeredAgent(mod)
	b, c := astral.GenerateIdentity(), astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(b, "b")

	// c writes first, so an unfiltered claim would take c's message
	storeThreaded(t, mod, c, me, testID(1), testID(1), "unrelated")
	storeThreaded(t, mod, b, me, testID(2), testID(9), "the answer")

	_, out, err := mod.readNextTool(me)(context.Background(), nil, readNextIn{
		Thread: testID(9).String(), TimeoutMs: 200,
	})
	if err != nil {
		t.Fatalf("read_next: %v", err)
	}
	if out.ID != testID(2).String() {
		t.Fatalf("claimed %v, want the message in the named thread", out.ID)
	}

	// the unrelated message is untouched and still claimable
	rows, err := mod.db.ListInbox(me, inboxQuery{UnreadOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != testID(1) {
		t.Fatalf("unread %+v, want the unrelated message still waiting", rows)
	}
}

// The same, by sender, and the sender is named the way send_message names a
// recipient — identity or alias.
func TestReadNextClaimsOnlyFromTheNamedSender(t *testing.T) {
	mod := testMessageModule(t)
	me := registeredAgent(mod)
	b, c := astral.GenerateIdentity(), astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(b, "b")

	storeThreaded(t, mod, c, me, testID(1), testID(1), "unrelated")
	storeThreaded(t, mod, b, me, testID(2), testID(2), "from b")

	for _, named := range []string{"b", b.String()} {
		mod2 := testMessageModule(t)
		me2 := registeredAgent(mod2)
		_ = mod2.Dir.SetAlias(b, "b")
		storeThreaded(t, mod2, c, me2, testID(1), testID(1), "unrelated")
		storeThreaded(t, mod2, b, me2, testID(2), testID(2), "from b")

		_, out, err := mod2.readNextTool(me2)(context.Background(), nil, readNextIn{
			From: named, TimeoutMs: 200,
		})
		if err != nil {
			t.Fatalf("read_next(%v): %v", named, err)
		}
		if out.ID != testID(2).String() {
			t.Fatalf("read_next(%v) claimed %v, want b's message", named, out.ID)
		}
	}
}

// A filter that matches nothing waits and then times out, rather than taking
// the nearest message.
func TestReadNextTimesOutRatherThanTakeAnother(t *testing.T) {
	mod := testMessageModule(t)
	me := registeredAgent(mod)
	storeThreaded(t, mod, astral.GenerateIdentity(), me, testID(1), testID(1), "unrelated")

	_, out, err := mod.readNextTool(me)(context.Background(), nil, readNextIn{
		Thread: testID(9).String(), TimeoutMs: 150,
	})
	if err != nil {
		t.Fatalf("read_next: %v", err)
	}
	if out.Status != "timeout" {
		t.Fatalf("status %q, want timeout", out.Status)
	}
	if rows, _ := mod.db.ListInbox(me, inboxQuery{UnreadOnly: true, Limit: 10}); len(rows) != 1 {
		t.Fatal("the unrelated message was claimed by a filter that did not name it")
	}
}

// An unknown sender is refused rather than silently widening to every sender.
func TestReadNextRefusesAnUnknownSender(t *testing.T) {
	mod := testMessageModule(t)
	me := registeredAgent(mod)
	storeThreaded(t, mod, astral.GenerateIdentity(), me, testID(1), testID(1), "one")

	if _, _, err := mod.readNextTool(me)(context.Background(), nil, readNextIn{
		From: "nobody", TimeoutMs: 100,
	}); err == nil {
		t.Fatal("an unresolvable sender was accepted")
	}
}

// inbox narrows to one exchange without claiming anything.
func TestInboxListsOneThread(t *testing.T) {
	mod := testMessageModule(t)
	me := registeredAgent(mod)
	other := astral.GenerateIdentity()

	storeThreaded(t, mod, other, me, testID(1), testID(9), "ask")
	storeThreaded(t, mod, other, me, testID(2), testID(9), "follow-up")
	storeThreaded(t, mod, other, me, testID(3), testID(3), "unrelated")

	_, out, err := mod.inboxTool(me)(context.Background(), nil, inboxIn{Thread: testID(9).String()})
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("listed %v, want the two in the thread", len(out.Messages))
	}
	for _, m := range out.Messages {
		if m.Thread != testID(9).String() {
			t.Fatalf("entry %+v is not in the named thread", m)
		}
	}
	if rows, _ := mod.db.ListInbox(me, inboxQuery{UnreadOnly: true, Limit: 10}); len(rows) != 3 {
		t.Fatal("listing claimed a message")
	}
}

// A sender reads its own side of one exchange the way the recipient reads
// theirs.
func TestOutboxListsOneThread(t *testing.T) {
	mod := testMessageModule(t)
	sender, _ := sendPair(t, mod)

	_, first, err := mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{To: "peer", Content: "one"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, _, err = mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "two", Thread: first.Thread,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, _, err = mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{To: "peer", Content: "elsewhere"}); err != nil {
		t.Fatalf("send: %v", err)
	}

	_, out, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{Thread: first.Thread})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 2 {
		t.Fatalf("listed %v, want the two sends in the thread", len(out.Messages))
	}
}

// A column added to a stored table is null on every row already there, and a
// null MessageID reads back as the zero value rather than an error — so the
// rows would be in no thread at all without the backfill.
func TestMigrateBackfillsThreadOnStoredRows(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	pool, _ := gdb.DB()
	pool.SetMaxOpenConns(1)
	db := &DB{DB: gdb}

	// the table as the schema before threads wrote it
	if err = db.Table(mcpmod.DBPrefix + "messages").AutoMigrate(&preThreadMessage{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err = db.Table(mcpmod.DBPrefix + "messages").Create(&preThreadMessage{
		ID:        testID(1),
		Sender:    astral.GenerateIdentity(),
		Recipient: astral.GenerateIdentity(),
		Content:   "one",
		StoredAt:  time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err = db.MigrateMessages(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var row dbMessage
	if err = db.First(&row).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if row.Thread.IsZero() {
		t.Fatal("a stored row is in no thread after the migration")
	}
	if row.Thread != row.ID {
		t.Fatalf("thread %v, want its own id %v", row.Thread, row.ID)
	}
}

// preThreadMessage is the inbox row before the thread column.
type preThreadMessage struct {
	ID        mcpapi.MessageID `gorm:"primaryKey"`
	Sender    *astral.Identity
	Recipient *astral.Identity
	Content   string
	StoredAt  time.Time
	ReadAt    *time.Time
}

// storeThreaded writes one inbox row in a named thread.
func storeThreaded(t *testing.T, mod *Module, sender, recipient *astral.Identity, id, thread mcpapi.MessageID, body string) {
	t.Helper()

	err := mod.storeMessage(sender, recipient, &mcpapi.Message{
		ID: id, Content: astral.String32(body), Thread: thread,
	})
	if err != nil {
		t.Fatalf("store %v: %v", id, err)
	}

	// arrival is stamped by the clock and the inbox is ordered by it
	time.Sleep(2 * time.Millisecond)
}
