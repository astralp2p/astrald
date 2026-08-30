package mcp

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	mcpmod "github.com/astralp2p/astrald/mod/mcp"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// sendPair registers a sender and a recipient this module answers for and gives
// the recipient an alias to send to.
func sendPair(t *testing.T, mod *Module) (sender, recipient *astral.Identity) {
	t.Helper()

	sender, recipient = registeredAgent(mod), registeredAgent(mod)
	if err := mod.Dir.SetAlias(recipient, "peer"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	return
}

// onlyOutboxRow returns the single row the sender holds, and fails on any other
// count: a test that reads the first of several rows says nothing about which.
func onlyOutboxRow(t *testing.T, mod *Module, sender *astral.Identity) dbOutbox {
	t.Helper()

	rows, err := mod.db.ListOutbox(sender, outboxQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%v rows in the outbox, want 1", len(rows))
	}
	return rows[0]
}

// A send that lands leaves the attempt, the acknowledgement and the body, and
// claims nothing about the recipient having read it.
func TestSendMessageRecordsADeliveryThatLanded(t *testing.T) {
	mod := testMessageModule(t)
	sender, _ := sendPair(t, mod)

	_, out, err := mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "run the release checks",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	row := onlyOutboxRow(t, mod, sender)
	if row.ID.String() != out.ID {
		t.Fatalf("row %v, tool answered %v", row.ID, out.ID)
	}
	if row.SentAt.IsZero() {
		t.Fatal("a row exists for a send that was never stamped attempted")
	}
	if row.StoredAt == nil {
		t.Fatal("an acknowledged delivery reads as unacknowledged")
	}
	if row.FailedAt != nil || row.Err != "" {
		t.Fatalf("a delivery that landed carries a failure: %+v", row)
	}
	if row.FetchedAt != nil {
		t.Fatal("a message nobody read reads as handed out")
	}
	if row.Content != "run the release checks" {
		t.Fatalf("content %q", row.Content)
	}
}

// The recipient's node judged the message and said no. The row carries its own
// words, which is what tells a judgement apart from a delivery that never
// arrived: the same bytes would get the same answer.
//
// why the two nodes carry different limits: a sender checks its own limit
// first, so a refusal at the store is only reachable where the recipient's is
// the smaller one.
func TestSendMessageRecordsARefusalWithItsWords(t *testing.T) {
	senderMod, recipientMod := twoNodes(t)
	recipientMod.config.MaxPayloadBytes = 8

	sender := registeredAgent(senderMod)
	recipient := registeredAgent(recipientMod)
	if err := senderMod.Dir.SetAlias(recipient, "peer"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	_, _, err := senderMod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: strings.Repeat("x", 64),
	})
	if err == nil {
		t.Fatal("a message the recipient refuses was accepted")
	}

	row := onlyOutboxRow(t, senderMod, sender)
	if row.FailedAt == nil {
		t.Fatal("a refused delivery reads as unresolved")
	}
	if !strings.Contains(row.Err, "message too large") {
		t.Fatalf("err %q, want the recipient's own words", row.Err)
	}
	if row.StoredAt != nil {
		t.Fatal("a refused delivery reads as stored")
	}
}

// A delivery that never left is a failure with no words: down, refusing or
// absent are indistinguishable by design.
func TestSendMessageRecordsADeliveryThatNeverLeft(t *testing.T) {
	mod := testMessageModule(t)
	sender := registeredAgent(mod)

	// the recipient resolves but is no agent this module answers for, so the
	// route is not found
	if err := mod.Dir.SetAlias(astral.GenerateIdentity(), "peer"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	_, _, err := mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "nobody is there",
	})
	if err == nil {
		t.Fatal("a delivery to nowhere was accepted")
	}

	row := onlyOutboxRow(t, mod, sender)
	if row.FailedAt == nil {
		t.Fatal("a delivery that never left reads as unresolved")
	}
	if row.Err != "" {
		t.Fatalf("a transport failure carries the recipient's words: %q", row.Err)
	}
	if row.StoredAt != nil {
		t.Fatal("a delivery that never left reads as stored")
	}
}

// A send refused before delivery leaves no row: a stored list of refusals would
// tell a recipient that refuses apart from one that does not exist.
func TestSendMessageRecordsNothingItRefusedItself(t *testing.T) {
	for _, c := range []struct {
		name  string
		allow bool
		to    string
	}{
		{"unresolvable recipient", true, "nobody"},
		{"the authority says no", false, "peer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod := testMessageModule(t)
			sender, _ := sendPair(t, mod)
			mod.Auth.(*fakeAuth).allow = c.allow

			_, _, err := mod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
				To: c.to, Content: "unreachable",
			})
			if err == nil {
				t.Fatal("the send was accepted")
			}

			rows, err := mod.db.ListOutbox(sender, outboxQuery{Limit: 10})
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(rows) != 0 {
				t.Fatalf("%v rows for a refusal that must leave none", len(rows))
			}
		})
	}
}

// The first answer is the one that happened, so a second stamp of the same
// column changes nothing and reports no error.
func TestOutboxStampsWriteOnce(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := mod.db.InsertOutbox(&dbOutbox{
		ID: testID(1), Sender: sender, Recipient: recipient, Content: "one",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := mod.db.StampOutboxStored(testID(1)); err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	first := onlyOutboxRow(t, mod, sender).StoredAt

	time.Sleep(5 * time.Millisecond)

	if err := mod.db.StampOutboxStored(testID(1)); err != nil {
		t.Fatalf("second stamp: %v", err)
	}
	if second := onlyOutboxRow(t, mod, sender).StoredAt; !second.Equal(*first) {
		t.Fatalf("stored_at moved from %v to %v", first, second)
	}
}

// A sender this node holds is stamped in its own table, with no query at all.
// Both reads do it, and neither does it twice.
func TestReadStampsALocalSenderOutbox(t *testing.T) {
	for _, c := range []struct {
		name string
		read func(t *testing.T, mod *Module, recipient *astral.Identity)
	}{
		{"read_next", func(t *testing.T, mod *Module, recipient *astral.Identity) {
			if _, _, err := mod.readNextTool(recipient)(context.Background(), nil, readNextIn{TimeoutMs: 200}); err != nil {
				t.Fatalf("read_next: %v", err)
			}
		}},
		{"read_message", func(t *testing.T, mod *Module, recipient *astral.Identity) {
			if _, _, err := mod.readMessageTool(recipient)(context.Background(), nil, readMessageIn{ID: testID(1).String()}); err != nil {
				t.Fatalf("read_message: %v", err)
			}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			mod := testMessageModule(t)
			sender, recipient := registeredAgent(mod), registeredAgent(mod)

			if err := mod.db.InsertOutbox(&dbOutbox{
				ID: testID(1), Sender: sender, Recipient: recipient, Content: "one",
			}); err != nil {
				t.Fatalf("insert outbox: %v", err)
			}
			storeOne(t, mod, sender, recipient, testID(1), "one")

			c.read(t, mod, recipient)

			row := onlyOutboxRow(t, mod, sender)
			if row.FetchedAt == nil {
				t.Fatal("a message handed out reads as never collected")
			}
			first := *row.FetchedAt

			time.Sleep(5 * time.Millisecond)
			c.read(t, mod, recipient)

			if again := onlyOutboxRow(t, mod, sender).FetchedAt; !again.Equal(first) {
				t.Fatalf("fetched_at moved from %v to %v", first, again)
			}

			// a local sender is told directly, so no receipt is ever owed
			inbox, err := mod.db.ListInbox(recipient, inboxQuery{Limit: 10})
			if err != nil {
				t.Fatalf("list inbox: %v", err)
			}
			if inbox[0].ReceiptDueAt != nil {
				t.Fatal("a local sender's message owes a receipt")
			}
		})
	}
}

// A claim that matches no outbox row succeeds and changes nothing: the sender
// may hold no row at all.
func TestReadWithNoOutboxRowSucceeds(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := registeredAgent(mod), registeredAgent(mod)

	storeOne(t, mod, sender, recipient, testID(1), "one")

	_, out, err := mod.readNextTool(recipient)(context.Background(), nil, readNextIn{TimeoutMs: 200})
	if err != nil {
		t.Fatalf("read_next: %v", err)
	}
	if out.Status != "message" {
		t.Fatalf("status %q, want a message", out.Status)
	}
	if rows, _ := mod.db.ListOutbox(sender, outboxQuery{Limit: 10}); len(rows) != 0 {
		t.Fatalf("%v rows appeared from a read", len(rows))
	}
}

// twoNodes builds a sender's node and a recipient's node with separate stores,
// the recipient's routing into the sender's. This is the shape the receipt path
// needs and the one a single node never takes.
func twoNodes(t *testing.T) (senderMod, recipientMod *Module) {
	t.Helper()

	senderMod = testMessageModule(t)

	recipientMod = &Module{
		Deps:   Deps{Auth: &fakeAuth{allow: true}},
		ctx:    astral.NewContext(nil),
		db:     testDB(t),
		config: defaultConfig,
		log:    testLogger(),
	}
	recipientMod.Dir = &stubDir{aliases: map[string]*astral.Identity{}}

	// each node reaches the other: a delivery crosses one way and the receipt
	// it earns crosses back.
	senderMod.node = &loopbackNode{identity: astral.GenerateIdentity(), router: recipientMod}
	recipientMod.node = &loopbackNode{identity: astral.GenerateIdentity(), router: senderMod}

	return
}

// The whole receipt path: the recipient's node hands the body out, puts an
// mcp.receipt to the sender's identity, and the sender's node stamps its own
// row collected.
func TestReceiptCarriesTheFetchAcrossNodes(t *testing.T) {
	senderMod, recipientMod := twoNodes(t)

	sender := registeredAgent(senderMod)
	recipient := registeredAgent(recipientMod)
	if err := senderMod.Dir.SetAlias(recipient, "peer"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	if _, _, err := senderMod.sendMessageTool(sender)(context.Background(), nil, sendMessageIn{
		To: "peer", Content: "run the release checks",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if onlyOutboxRow(t, senderMod, sender).FetchedAt != nil {
		t.Fatal("a message nobody read reads as collected")
	}

	if _, _, err := recipientMod.readNextTool(recipient)(context.Background(), nil, readNextIn{TimeoutMs: 200}); err != nil {
		t.Fatalf("read_next: %v", err)
	}

	// the receipt rides a goroutine the read never waits on
	waitFor(t, "the sender's row is stamped collected", func() bool {
		return onlyOutboxRow(t, senderMod, sender).FetchedAt != nil
	})

	inbox, err := recipientMod.db.ListInbox(recipient, inboxQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if inbox[0].ReceiptDueAt == nil {
		t.Fatal("a remote sender's message owes no receipt")
	}
	waitFor(t, "the receipt is acknowledged", func() bool {
		rows, _ := recipientMod.db.ListInbox(recipient, inboxQuery{Limit: 10})
		return rows[0].ReceiptStoredAt != nil
	})
}

// One attempt is made, and it belongs to whichever read first handed the body
// out. A second read finds the row already due and sends nothing.
func TestReceiptIsOwedOnce(t *testing.T) {
	senderMod, recipientMod := twoNodes(t)

	sender := registeredAgent(senderMod)
	recipient := registeredAgent(recipientMod)

	storeOne(t, recipientMod, sender, recipient, testID(1), "one")

	if _, _, err := recipientMod.readMessageTool(recipient)(context.Background(), nil, readMessageIn{ID: testID(1).String()}); err != nil {
		t.Fatalf("first read: %v", err)
	}
	rows, err := recipientMod.db.ListInbox(recipient, inboxQuery{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	first := *rows[0].ReceiptDueAt

	time.Sleep(5 * time.Millisecond)

	if _, _, err = recipientMod.readMessageTool(recipient)(context.Background(), nil, readMessageIn{ID: testID(1).String()}); err != nil {
		t.Fatalf("second read: %v", err)
	}
	rows, _ = recipientMod.db.ListInbox(recipient, inboxQuery{Limit: 10})
	if !rows[0].ReceiptDueAt.Equal(first) {
		t.Fatalf("receipt_due_at moved from %v to %v", first, rows[0].ReceiptDueAt)
	}
}

// receiptOverRouter drives one receipt through RouteQuery the way a recipient's
// node does, and answers what came back.
func receiptOverRouter(t *testing.T, mod *Module, caller, target *astral.Identity, id mcpapi.MessageID) astral.Object {
	t.Helper()

	w := &bufWriteCloser{}
	q := astral.Launch(astral.NewQuery(caller, target, mcpapi.MethodReceipt))

	wc, err := mod.RouteQuery(mod.ctx, q, w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if err = channel.NewSender(wc).Send(&mcpapi.Receipt{ID: id}); err != nil {
		t.Fatalf("send receipt: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for w.String() == "" {
		if time.Now().After(deadline) {
			t.Fatal("no answer to the receipt")
		}
		time.Sleep(5 * time.Millisecond)
	}

	obj, err := channel.NewReceiver(bytes.NewReader([]byte(w.String()))).Receive()
	if err != nil {
		t.Fatalf("receive answer: %v", err)
	}
	return obj
}

// The row is the admission: a receipt about a message this agent never sent to
// this caller is refused, and stamps nothing.
func TestAcceptReceiptRefusesARowThatIsNotItsOwn(t *testing.T) {
	mod := testMessageModule(t)
	sender := registeredAgent(mod)
	recipient := astral.GenerateIdentity()

	if err := mod.db.InsertOutbox(&dbOutbox{
		ID: testID(1), Sender: sender, Recipient: recipient, Content: "one",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	for _, c := range []struct {
		name   string
		caller *astral.Identity
		id     mcpapi.MessageID
	}{
		{"a caller the message never went to", astral.GenerateIdentity(), testID(1)},
		{"a message id nothing was sent under", recipient, testID(9)},
	} {
		t.Run(c.name, func(t *testing.T) {
			obj := receiptOverRouter(t, mod, c.caller, sender, c.id)
			if _, ok := obj.(astral.Error); !ok {
				t.Fatalf("receipt answered %T, want an error", obj)
			}
			if onlyOutboxRow(t, mod, sender).FetchedAt != nil {
				t.Fatal("a refused receipt stamped the row")
			}
		})
	}
}

// The matching row is stamped and acknowledged, and a receipt repeated over it
// changes nothing.
func TestAcceptReceiptStampsTheMatchingRow(t *testing.T) {
	mod := testMessageModule(t)
	sender := registeredAgent(mod)
	recipient := astral.GenerateIdentity()

	if err := mod.db.InsertOutbox(&dbOutbox{
		ID: testID(1), Sender: sender, Recipient: recipient, Content: "one",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if obj := receiptOverRouter(t, mod, recipient, sender, testID(1)); !isAck(obj) {
		t.Fatalf("receipt answered %T, want an ack", obj)
	}
	first := *onlyOutboxRow(t, mod, sender).FetchedAt

	// a receipt that arrives twice stamps once, and the second is refused
	// because the row is no longer unstamped
	if obj := receiptOverRouter(t, mod, recipient, sender, testID(1)); isAck(obj) {
		t.Fatal("a second receipt was acknowledged")
	}
	if again := onlyOutboxRow(t, mod, sender).FetchedAt; !again.Equal(first) {
		t.Fatalf("fetched_at moved from %v to %v", first, again)
	}
}

// An agent's sent list is its own. Another agent's rows are not in it.
func TestOutboxToolListsOnlyTheCallersRows(t *testing.T) {
	mod := testMessageModule(t)
	mine, other := astral.GenerateIdentity(), astral.GenerateIdentity()
	recipient := astral.GenerateIdentity()
	_ = mod.Dir.SetAlias(recipient, "peer")

	for _, c := range []struct {
		id     mcpapi.MessageID
		sender *astral.Identity
	}{{testID(1), mine}, {testID(2), other}} {
		if err := mod.db.InsertOutbox(&dbOutbox{
			ID: c.id, Sender: c.sender, Recipient: recipient, Content: "one",
		}); err != nil {
			t.Fatalf("insert %v: %v", c.id, err)
		}
	}

	_, out, err := mod.outboxTool(mine)(context.Background(), nil, outboxIn{})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 1 || out.Messages[0].ID != testID(1).String() {
		t.Fatalf("listed %+v, want the caller's row alone", out.Messages)
	}

	entry := out.Messages[0]
	if entry.Recipient != recipient.String() || entry.RecipientAlias != "peer" {
		t.Fatalf("recipient %v/%v", entry.Recipient, entry.RecipientAlias)
	}
	if entry.SentAt == "" {
		t.Fatal("a listed row carries no attempt time")
	}
	// an instant that never happened is absent rather than zero
	if entry.StoredAt != "" || entry.FailedAt != "" || entry.FetchedAt != "" {
		t.Fatalf("a fresh row claims an outcome: %+v", entry)
	}
}

// oldDBMessage is the inbox row before the rename: delivered_at, and neither
// receipt column.
type oldDBMessage struct {
	ID          mcpapi.MessageID `gorm:"primaryKey"`
	Sender      string
	Recipient   string
	Content     string
	DeliveredAt time.Time
	ReadAt      *time.Time
}

// AutoMigrate adds a column and never renames one, so the rename is its own
// step and the instants already stored survive it.
func TestMigrateMessagesRenamesDeliveredAt(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db := &DB{DB: gdb}

	// the table as the previous schema wrote it, built the way that schema
	// built it — a hand-written DDL is not what the migrator reads back
	if err = db.Table(mcpmod.DBPrefix + "messages").AutoMigrate(&oldDBMessage{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	stored := time.Now().UTC().Truncate(time.Second)
	if err = db.Table(mcpmod.DBPrefix + "messages").Create(&oldDBMessage{
		ID: testID(1), Sender: "s", Recipient: "r", Content: "one", DeliveredAt: stored,
	}).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err = db.MigrateMessages(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	m := db.Migrator()
	if m.HasColumn(&dbMessage{}, "delivered_at") {
		t.Fatal("delivered_at survived the rename")
	}
	if !m.HasColumn(&dbMessage{}, "receipt_due_at") || !m.HasColumn(&dbMessage{}, "receipt_stored_at") {
		t.Fatal("the receipt columns were not added")
	}

	var got time.Time
	if err = db.Raw(`SELECT stored_at FROM mcp__messages WHERE id = ?`, testID(1).String()).Scan(&got).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !got.UTC().Truncate(time.Second).Equal(stored) {
		t.Fatalf("stored_at %v, want the delivered_at it was renamed from (%v)", got, stored)
	}
}

// waitFor polls a condition the read does not wait on itself.
func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %v", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func isAck(obj astral.Object) bool {
	_, ok := obj.(*astral.Ack)
	return ok
}

// seedOutbox writes one row and stamps it into the state the case needs.
func seedOutbox(t *testing.T, mod *Module, sender *astral.Identity, id mcpapi.MessageID, stored, fetched bool) {
	t.Helper()

	if err := mod.db.InsertOutbox(&dbOutbox{
		ID: id, Sender: sender, Recipient: astral.GenerateIdentity(), Content: "x",
	}); err != nil {
		t.Fatalf("insert %v: %v", id, err)
	}
	if stored {
		if err := mod.db.StampOutboxStored(id); err != nil {
			t.Fatalf("stamp stored %v: %v", id, err)
		}
	}
	if fetched {
		if err := mod.db.StampOutboxFetched(id); err != nil {
			t.Fatalf("stamp fetched %v: %v", id, err)
		}
	}

	// arrival is stamped by the clock and the list is ordered by it
	time.Sleep(2 * time.Millisecond)
}

// send_message answers an id, so the id is a handle: it names one send and
// answers that send alone.
func TestOutboxAnswersOneSendByID(t *testing.T) {
	mod := testMessageModule(t)
	sender := astral.GenerateIdentity()

	seedOutbox(t, mod, sender, testID(1), true, false)
	seedOutbox(t, mod, sender, testID(2), true, true)

	_, out, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{ID: testID(2).String()})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 1 || out.Messages[0].ID != testID(2).String() {
		t.Fatalf("listed %+v, want that send alone", out.Messages)
	}

	// an id belonging to nobody answers nothing rather than everything
	_, out, err = mod.outboxTool(sender)(context.Background(), nil, outboxIn{ID: testID(9).String()})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 0 {
		t.Fatalf("an unknown id listed %v rows", len(out.Messages))
	}
}

// An id is not a way around the sender scope: another agent's send is not one
// this agent can name.
func TestOutboxByIDStaysWithinTheCaller(t *testing.T) {
	mod := testMessageModule(t)
	mine, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	seedOutbox(t, mod, other, testID(1), true, false)

	_, out, err := mod.outboxTool(mine)(context.Background(), nil, outboxIn{ID: testID(1).String()})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 0 {
		t.Fatal("another agent's send is readable by id")
	}
}

// The orchestrator's question: which sends are in a mailbox and not yet taken.
// A delivery that failed waits on nobody, and one already collected is done.
func TestOutboxAwaitingPickupIsStoredAndNotFetched(t *testing.T) {
	mod := testMessageModule(t)
	sender := astral.GenerateIdentity()

	seedOutbox(t, mod, sender, testID(1), true, false)  // waiting on them
	seedOutbox(t, mod, sender, testID(2), true, true)   // collected
	seedOutbox(t, mod, sender, testID(3), false, false) // fate unknown
	seedOutbox(t, mod, sender, testID(4), false, false)
	if err := mod.db.StampOutboxFailed(testID(4)); err != nil { // did not land
		t.Fatalf("stamp failed: %v", err)
	}

	_, out, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{AwaitingPickup: true})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if len(out.Messages) != 1 || out.Messages[0].ID != testID(1).String() {
		t.Fatalf("listed %+v, want the stored-and-uncollected send alone", out.Messages)
	}
}

// A sent list reads newest first, and the sends worth chasing are the oldest —
// exactly the rows a newest-first cap drops.
func TestOutboxReachesTheOldestSends(t *testing.T) {
	mod := testMessageModule(t)
	sender := astral.GenerateIdentity()

	for i := range 5 {
		seedOutbox(t, mod, sender, testID(byte(i+1)), true, false)
	}

	_, newest, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{Limit: 2})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if newest.Messages[0].ID != testID(5).String() {
		t.Fatalf("first newest-first entry %v, want the last send", newest.Messages[0].ID)
	}

	_, oldest, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{Limit: 2, OldestFirst: true})
	if err != nil {
		t.Fatalf("outbox: %v", err)
	}
	if oldest.Messages[0].ID != testID(1).String() {
		t.Fatalf("first oldest-first entry %v, want the first send", oldest.Messages[0].ID)
	}
}

// A malformed id is refused rather than read as "list everything".
func TestOutboxRefusesAMalformedID(t *testing.T) {
	mod := testMessageModule(t)
	sender := astral.GenerateIdentity()
	seedOutbox(t, mod, sender, testID(1), true, false)

	if _, _, err := mod.outboxTool(sender)(context.Background(), nil, outboxIn{ID: "not-an-id"}); err == nil {
		t.Fatal("a malformed id was accepted")
	}
}
