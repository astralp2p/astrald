package messaging

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
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

// testMessagingModule is the module over a fresh store, on a node whose own
// key the keyring holds. Whether this node hosts a mailbox is answered by the
// real auth module over the same store, with this module's root rule
// registered; every other question is answered yes.
func testMessagingModule(t *testing.T) *Module {
	t.Helper()

	keys := newKeyring()

	mod := &Module{
		db:     testDB(t),
		config: defaultConfig,
		log:    testLogger(),
	}
	wireTestModule(t, mod, keys.mint(), keys)

	return mod
}

// wireTestModule gives a module what its dependencies give it on a node: a
// context, the node nodeID names, the directory, crypto and object doubles,
// and the real auth module over the module's store with the hosting root
// registered.
func wireTestModule(t *testing.T, mod *Module, nodeID *astral.Identity, keys *keyring) {
	t.Helper()

	mod.ctx = astral.NewContext(nil)
	mod.Dir = &stubDir{aliases: map[string]*astral.Identity{}}
	mod.Crypto = keys
	mod.Objects = &stubObjects{}
	mod.node = &loopbackNode{identity: nodeID, router: mod}
	mod.Auth = &fakeAuth{Module: testAuthority(t, mod.db.DB, mod.node, keys), allow: true}
	mod.addAuthorizers()
}

// loadOver starts the module over a store the way a node does — the loader's
// migration and index load, then the dependencies — on the node nodeID names,
// whose keys the keyring holds, with auth loaded afresh over the same store.
func loadOver(t *testing.T, db *DB, nodeID *astral.Identity, keys *keyring) *Module {
	t.Helper()

	loaded, err := Loader{}.Load(nil, testAssets{db: db.DB}, testLogger())
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	mod := loaded.(*Module)
	wireTestModule(t, mod, nodeID, keys)

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

// testDB opens an empty store with the module's tables.
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
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// testID makes an identifier a failure can name.
func testID(n byte) messaging.MessageID {
	var id messaging.MessageID
	id[0] = n
	return id
}

func deliverOverRouter(t *testing.T, mod *Module, recipient *astral.Identity, msg *messaging.Message) astral.Object {
	t.Helper()

	w := &bufWriteCloser{}

	wc, err := mod.RouteQuery(mod.ctx, inFlight(recipient, messaging.MethodMessage), w)
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
