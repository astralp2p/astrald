package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// testLogger emits nothing: the module logs every delivery, and a test
// asserting on the store has no use for the line.
func testLogger() *log.Logger {
	l := log.New(astral.GenerateIdentity())
	l.SetFilter(func(*log.Entry) bool { return false })
	return l
}

func testMessageModule(t *testing.T) *Module {
	t.Helper()

	db := testDB(t)

	mod := &Module{
		Deps:   Deps{Auth: &fakeAuth{allow: true}},
		ctx:    astral.NewContext(nil),
		db:     db,
		config: defaultConfig,
		log:    testLogger(),
	}
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{}}
	mod.node = &loopbackNode{identity: astral.GenerateIdentity(), router: mod}

	return mod
}

// loopbackNode routes every query to one module, which is what makes a delivery
// and a receipt reachable in a test: the caller's side and the answering side
// are the same code, and only the identities differ.
type loopbackNode struct {
	identity *astral.Identity
	router   astral.Router
}

func (n *loopbackNode) Identity() *astral.Identity { return n.identity }

func (n *loopbackNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return n.router.RouteQuery(ctx, q, w)
}

// testDB opens an empty store with both tables.
//
// why the pool is capped at one connection: an in-memory sqlite database
// belongs to the connection that opened it, so a second pooled connection is a
// second, empty database. The receipt runs on a goroutine the read does not
// wait on, which is exactly what opens one.
func testDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	pool, err := gdb.DB()
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	pool.SetMaxOpenConns(1)

	db := &DB{DB: gdb}
	if err = db.MigrateMessages(); err != nil {
		t.Fatalf("migrate messages: %v", err)
	}
	if err = db.MigrateOutbox(); err != nil {
		t.Fatalf("migrate outbox: %v", err)
	}
	return db
}

// testID makes an identifier a failure can name.
func testID(n byte) mcpapi.MessageID {
	var id mcpapi.MessageID
	id[0] = n
	return id
}

func storeOne(t *testing.T, mod *Module, sender, recipient *astral.Identity, id mcpapi.MessageID, body string) {
	t.Helper()

	err := mod.storeMessage(sender, recipient, &mcpapi.Message{
		ID:      id,
		Content: astral.String32(body),
	})
	if err != nil {
		t.Fatalf("store %v: %v", id, err)
	}

	// why the pause: arrival is stamped by the clock, and the inbox is ordered
	// by it. Two messages stored in the same instant have no oldest.
	time.Sleep(2 * time.Millisecond)
}

// A sender that retries after a lost acknowledgement sends the same id twice,
// and the recipient must hold one message rather than two.
func TestInsertMessageStoresOnce(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "first")
	storeOne(t, mod, sender, recipient, testID(1), "second")

	rows, err := mod.db.ListInbox(recipient, false, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("%v messages stored, want 1", len(rows))
	}
	if rows[0].Content != "first" {
		t.Fatalf("content %v, want the first delivery to stand", rows[0].Content)
	}
}

func TestListInboxOrdersAndFilters(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")
	storeOne(t, mod, sender, recipient, testID(2), "two")
	storeOne(t, mod, sender, astral.GenerateIdentity(), testID(3), "elsewhere")

	rows, err := mod.db.ListInbox(recipient, false, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].ID != testID(1) || rows[1].ID != testID(2) {
		t.Fatalf("listed %v, want a then b", rows)
	}

	if _, err = mod.db.ReadMessage(recipient, testID(1)); err != nil {
		t.Fatalf("read: %v", err)
	}

	rows, err = mod.db.ListInbox(recipient, true, 10)
	if err != nil {
		t.Fatalf("list unread: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != testID(2) {
		t.Fatalf("unread %v, want b alone", rows)
	}
}

// Reading is not claiming: a second read answers the same message, and the
// stamp records the first read rather than the last.
func TestReadMessageStampsOnce(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")

	first, err := mod.db.ReadMessage(recipient, testID(1))
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if first.ReadAt == nil {
		t.Fatal("first read left the message unread")
	}

	time.Sleep(5 * time.Millisecond)

	second, err := mod.db.ReadMessage(recipient, testID(1))
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !second.ReadAt.Equal(*first.ReadAt) {
		t.Fatalf("read_at moved from %v to %v", first.ReadAt, second.ReadAt)
	}
}

// An inbox holds one agent's messages, and an id another agent was sent is not
// one this agent can name.
func TestReadMessageScopesToRecipient(t *testing.T) {
	mod := testMessageModule(t)
	sender := astral.GenerateIdentity()

	storeOne(t, mod, sender, astral.GenerateIdentity(), testID(1), "one")

	_, err := mod.db.ReadMessage(astral.GenerateIdentity(), testID(1))
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("read: got %v, want gorm.ErrRecordNotFound", err)
	}
}

// Each claim takes a different message and the last finds none: this is what
// makes a message delivered exactly once to a reader.
func TestClaimNextTakesEachMessageOnce(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	storeOne(t, mod, sender, recipient, testID(1), "one")
	storeOne(t, mod, sender, recipient, testID(2), "two")

	for _, want := range []mcpapi.MessageID{testID(1), testID(2)} {
		row, err := mod.db.ClaimNext(recipient)
		if err != nil {
			t.Fatalf("claim %v: %v", want, err)
		}
		if row.ID != want {
			t.Fatalf("claimed %v, want %v", row.ID, want)
		}
		if row.ReadAt == nil {
			t.Fatalf("claim of %v left it unread", row.ID)
		}
	}

	if _, err := mod.db.ClaimNext(recipient); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("third claim: got %v, want gorm.ErrRecordNotFound", err)
	}
}

// A reader waiting on an empty inbox takes the message that lands while it
// waits, which is what lets a caller reach an agent that was not reading.
func TestClaimNextWaitsForDelivery(t *testing.T) {
	mod := testMessageModule(t)
	sender, recipient := astral.GenerateIdentity(), astral.GenerateIdentity()

	go func() {
		time.Sleep(100 * time.Millisecond)
		storeOne(t, mod, sender, recipient, testID(1), "late")
	}()

	row, err := mod.claimNext(context.Background(), recipient, 3*time.Second)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if row.ID != testID(1) {
		t.Fatalf("claimed %v, want %v", row.ID, testID(1))
	}
}

func TestClaimNextTimesOutEmpty(t *testing.T) {
	mod := testMessageModule(t)

	_, err := mod.claimNext(context.Background(), astral.GenerateIdentity(), 50*time.Millisecond)
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("claim: got %v, want gorm.ErrRecordNotFound", err)
	}
}

// deliverOverRouter drives one delivery through RouteQuery the way a caller
// does, and answers what came back.
func deliverOverRouter(t *testing.T, mod *Module, recipient *astral.Identity, msg *mcpapi.Message) astral.Object {
	t.Helper()

	w := &bufWriteCloser{}

	wc, err := mod.RouteQuery(mod.ctx, inFlight(recipient, mcpapi.MethodMessage), w)
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if err = channel.NewSender(wc).Send(msg); err != nil {
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
	return obj
}

// The whole cross-node path: a query addressed to an agent stores a message and
// acknowledges the write, with no listener parked and no session opened.
func TestRouteQueryStoresMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcpapi.Message{
		ID:      testID(1),
		Content: astral.String32("the index is rebuilt"),
	})

	if _, ok := obj.(*astral.Ack); !ok {
		t.Fatalf("delivery answered %T, want an ack", obj)
	}

	row, err := mod.db.ReadMessage(recipient, testID(1))
	if err != nil {
		t.Fatalf("read stored: %v", err)
	}
	if row.Content != "the index is rebuilt" {
		t.Fatalf("stored %+v", row)
	}
}

func TestRouteQueryRefusesOversizeMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcpapi.Message{
		ID:      testID(1),
		Content: astral.String32(bytes.Repeat([]byte("x"), mod.config.MaxPayloadBytes+1)),
	})

	if _, ok := obj.(astral.Error); !ok {
		t.Fatalf("delivery answered %T, want an error", obj)
	}

	rows, err := mod.db.ListInbox(recipient, false, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("%v messages stored despite the refusal", len(rows))
	}
}
