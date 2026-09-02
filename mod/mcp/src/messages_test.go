package mcp

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
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
	if err = db.Migrate(); err != nil {
		t.Fatalf("migrate messages: %v", err)
	}
	return db
}

// testID makes an identifier a failure can name.
func testID(n byte) mcp.MessageID {
	var id mcp.MessageID
	id[0] = n
	return id
}

func storeOne(t *testing.T, mod *Module, sender, recipient *astral.Identity, id mcp.MessageID, body string) {
	t.Helper()

	err := mod.storeMessage(sender, recipient, &mcp.Message{
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

func deliverOverRouter(t *testing.T, mod *Module, recipient *astral.Identity, msg *mcp.Message) astral.Object {
	t.Helper()

	w := &bufWriteCloser{}

	wc, err := mod.RouteQuery(mod.ctx, inFlight(recipient, mcp.MethodMessage), w)
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

// A sender that retries after a lost acknowledgement sends the same id twice,
// and the recipient must hold one message rather than two.
func TestRouteQueryStoresMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcp.Message{
		ID:      testID(1),
		Content: astral.String32("the index is rebuilt"),
	})

	if _, ok := obj.(*astral.Ack); !ok {
		t.Fatalf("delivery answered %T, want an ack", obj)
	}

	rows, _, err := mod.db.ReadMany(recipient, []messageRef{{Box: boxInbox, ID: testID(1)}})
	if err != nil {
		t.Fatalf("read stored: %v", err)
	}
	if len(rows) != 1 || rows[0].Content != "the index is rebuilt" {
		t.Fatalf("stored %+v", rows)
	}
}

func TestRouteQueryRefusesOversizeMessage(t *testing.T) {
	mod := testMessageModule(t)
	recipient := astral.GenerateIdentity()
	_ = mod.agentIDs.Add(recipient.String())

	obj := deliverOverRouter(t, mod, recipient, &mcp.Message{
		ID:      testID(1),
		Content: astral.String32(bytes.Repeat([]byte("x"), mod.config.MaxPayloadBytes+1)),
	})

	if _, ok := obj.(astral.Error); !ok {
		t.Fatalf("delivery answered %T, want an error", obj)
	}

	rows, err := mod.listMessages(recipient, listRequest{List: listInbox})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("%v messages stored despite the refusal", len(rows))
	}
}
