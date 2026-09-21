package apphost

import (
	"bytes"
	"crypto/sha256"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// holdModule returns a Module whose database holds only the object hold table.
func holdModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbObjectHold{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{db: db, log: log.New(nil)}
}

// replyWriter is the caller's end of the connection. It keeps the bytes the op answered.
type replyWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	closed  chan struct{}
	closing atomic.Bool
}

func newReplyWriter() *replyWriter { return &replyWriter{closed: make(chan struct{})} }

func (w *replyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *replyWriter) Close() error {
	if w.closing.CompareAndSwap(false, true) {
		close(w.closed)
	}
	return nil
}

// reply waits for the op to close the connection and decodes the first object it answered.
func (w *replyWriter) reply(t *testing.T) astral.Object {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the op never closed the caller's connection")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	object, err := channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
	if err != nil {
		t.Fatalf("decode reply: %v", err)
	}
	return object
}

// heldFullID is the full ID of an object an app holds; its partial form names the same object.
func heldFullID() *astral.ObjectID {
	id := &astral.ObjectID{Size: 40}
	for i := range id.Hash {
		id.Hash[i] = byte(i)
	}
	return id
}

// checkErrorReply fails the test unless reply is an error message carrying want.
func checkErrorReply(t *testing.T, reply astral.Object, want error) {
	t.Helper()

	msg, ok := reply.(*astral.ErrorMessage)
	if !ok {
		t.Fatalf("reply is %T; want an error message", reply)
	}
	if msg.Error() != want.Error() {
		t.Fatalf("reply %q; want %q", msg.Error(), want.Error())
	}
}

// checkHoldCount fails the test unless app holds exactly n objects.
func checkHoldCount(t *testing.T, mod *Module, app *astral.Identity, n int) {
	t.Helper()

	holds, err := mod.db.ListHeldObjects(app)
	if err != nil {
		t.Fatalf("list holds: %v", err)
	}
	if len(holds) != n {
		t.Fatalf("app holds %d objects; want %d", len(holds), n)
	}
}

// TestHoldObjectRefusesPartialID: a hold keyed by a partial ID never matches the
// full ID purge asks about, so apphost.hold_object refuses it and stores nothing.
func TestHoldObjectRefusesPartialID(t *testing.T) {
	mod := holdModule(t)
	app, full := astral.GenerateIdentity(), heldFullID()
	partial := &astral.ObjectID{Hash: full.Hash}

	w := newReplyWriter()
	if err := route(t, mod.OpHoldObject, app, "apphost.hold_object?id="+partial.PartialString(), w); err != nil {
		t.Fatalf("apphost.hold_object: %v", err)
	}

	checkErrorReply(t, w.reply(t), objects.ErrPartialObjectID)
	checkHoldCount(t, mod, app, 0)
	if mod.HoldObject(full) {
		t.Fatal("a refused partial hold protects the object")
	}
}

// TestUnholdObjectRefusesPartialID: apphost.unhold_object refuses a partial ID and
// deletes nothing, so the hold keyed by the full ID still protects the object from purge.
func TestUnholdObjectRefusesPartialID(t *testing.T) {
	mod := holdModule(t)
	app, full := astral.GenerateIdentity(), heldFullID()
	partial := &astral.ObjectID{Hash: full.Hash}

	if reply := mod.holdOne(app, full, nil); reply.ObjectType() != (&astral.Ack{}).ObjectType() {
		t.Fatalf("hold by full ID answered %v; want ack", reply)
	}

	w := newReplyWriter()
	if err := route(t, mod.OpUnholdObject, app, "apphost.unhold_object?id="+partial.PartialString(), w); err != nil {
		t.Fatalf("apphost.unhold_object: %v", err)
	}

	checkErrorReply(t, w.reply(t), objects.ErrPartialObjectID)
	checkHoldCount(t, mod, app, 1)
	if !mod.HoldObject(full) {
		t.Fatal("the hold keyed by the full ID no longer protects the object")
	}
}

// TestHoldAndUnholdOneRefuseSizeZeroIDs covers the per-ID step that both the single
// and the batch mode run. The zero ID keeps its missing-ID error. The empty object's
// full ID has Size 0 and is refused like a partial ID.
func TestHoldAndUnholdOneRefuseSizeZeroIDs(t *testing.T) {
	empty := &astral.ObjectID{Hash: sha256.Sum256(nil)}

	cases := []struct {
		name string
		id   *astral.ObjectID
		want error
	}{
		{name: "zero ID", id: &astral.ObjectID{}, want: apphost.ErrMissingObjectID},
		{name: "partial ID", id: &astral.ObjectID{Hash: heldFullID().Hash}, want: objects.ErrPartialObjectID},
		{name: "empty object", id: empty, want: objects.ErrPartialObjectID},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := holdModule(t)
			app := astral.GenerateIdentity()

			checkErrorReply(t, mod.holdOne(app, c.id, nil), c.want)
			checkErrorReply(t, mod.unholdOne(app, c.id), c.want)
			checkHoldCount(t, mod, app, 0)
		})
	}
}
