package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testHoldModule(t *testing.T) *Module {
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

func holdID(n byte) *astral.ObjectID {
	return &astral.ObjectID{Size: 1, Hash: [32]byte{n}}
}

func holdFor(d time.Duration) *astral.Duration {
	v := astral.Duration(d)
	return &v
}

func hold(t *testing.T, mod *Module, app *astral.Identity, id *astral.ObjectID, d *astral.Duration) {
	t.Helper()

	if err := mod.db.HoldObject(app, id, d); err != nil {
		t.Fatalf("hold %v for %v: %v", id, app, err)
	}
}

func unhold(t *testing.T, mod *Module, app *astral.Identity, id *astral.ObjectID) {
	t.Helper()

	if err := mod.db.UnholdObject(app, id); err != nil {
		t.Fatalf("unhold %v for %v: %v", id, app, err)
	}
}

func requireHeld(t *testing.T, mod *Module, id *astral.ObjectID, want bool) {
	t.Helper()

	held, err := mod.db.ObjectHeld(id)
	if err != nil {
		t.Fatalf("ObjectHeld: %v", err)
	}
	if held != want {
		t.Fatalf("ObjectHeld(%v) = %v, want %v", id, held, want)
	}
}

func listHeld(t *testing.T, mod *Module, app *astral.Identity) []*dbObjectHold {
	t.Helper()

	rows, err := mod.db.ListHeldObjects(app)
	if err != nil {
		t.Fatalf("ListHeldObjects: %v", err)
	}
	return rows
}

func TestHoldObjectPermanent(t *testing.T) {
	mod := testHoldModule(t)
	app, id := astral.GenerateIdentity(), holdID(1)

	hold(t, mod, app, id, nil)

	requireHeld(t, mod, id, true)
	rows := listHeld(t, mod, app)
	if len(rows) != 1 || !rows[0].ObjectID.IsEqual(id) {
		t.Fatalf("ListHeldObjects = %v, want one row for %v", rows, id)
	}
	if !mod.HoldObject(id) {
		t.Fatal("Module.HoldObject = false, want true")
	}
}

func TestHoldObjectDuration(t *testing.T) {
	cases := []struct {
		name     string
		duration time.Duration
		want     bool
	}{
		{"unexpired", time.Hour, true},
		{"lapsed", -time.Minute, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod := testHoldModule(t)
			app, id := astral.GenerateIdentity(), holdID(1)

			hold(t, mod, app, id, holdFor(c.duration))

			requireHeld(t, mod, id, c.want)
			wantRows := 0
			if c.want {
				wantRows = 1
			}
			if rows := listHeld(t, mod, app); len(rows) != wantRows {
				t.Fatalf("ListHeldObjects has %d rows, want %d", len(rows), wantRows)
			}
		})
	}
}

func TestHoldObjectTwiceKeepsOneRow(t *testing.T) {
	mod := testHoldModule(t)
	app, id := astral.GenerateIdentity(), holdID(1)

	hold(t, mod, app, id, nil)
	hold(t, mod, app, id, nil)

	if rows := listHeld(t, mod, app); len(rows) != 1 {
		t.Fatalf("ListHeldObjects has %d rows, want 1", len(rows))
	}
}

func TestUnholdObjectReleasesOnlyTheCallersHold(t *testing.T) {
	mod := testHoldModule(t)
	appA, appB, id := astral.GenerateIdentity(), astral.GenerateIdentity(), holdID(1)

	hold(t, mod, appA, id, nil)
	hold(t, mod, appB, id, nil)

	unhold(t, mod, appA, id)
	requireHeld(t, mod, id, true)

	unhold(t, mod, appB, id)
	requireHeld(t, mod, id, false)
}

func TestUnholdObjectWithoutAHold(t *testing.T) {
	mod := testHoldModule(t)

	if err := mod.db.UnholdObject(astral.GenerateIdentity(), holdID(1)); err != nil {
		t.Fatalf("UnholdObject without a hold: %v, want nil", err)
	}
}

func TestListHeldObjectsIsScopedToTheApp(t *testing.T) {
	mod := testHoldModule(t)
	appA, appB := astral.GenerateIdentity(), astral.GenerateIdentity()

	hold(t, mod, appA, holdID(1), nil)
	hold(t, mod, appB, holdID(2), nil)

	rows := listHeld(t, mod, appA)
	if len(rows) != 1 || !rows[0].ObjectID.IsEqual(holdID(1)) {
		t.Fatalf("ListHeldObjects(appA) = %v, want only %v", rows, holdID(1))
	}
}

func TestHoldOneAndUnholdOneReplies(t *testing.T) {
	app := astral.GenerateIdentity()
	ops := []struct {
		name string
		call func(*Module, *astral.ObjectID) astral.Object
	}{
		{"holdOne", func(mod *Module, id *astral.ObjectID) astral.Object { return mod.holdOne(app, id, nil) }},
		{"unholdOne", func(mod *Module, id *astral.ObjectID) astral.Object { return mod.unholdOne(app, id) }},
	}

	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			mod := testHoldModule(t)

			reply := op.call(mod, &astral.ObjectID{})
			msg, ok := reply.(*astral.ErrorMessage)
			if !ok {
				t.Fatalf("zero id: reply %T, want *astral.ErrorMessage", reply)
			}
			if msg.Error() != apphostmod.ErrMissingObjectID.Error() {
				t.Fatalf("zero id: error %q, want %q", msg.Error(), apphostmod.ErrMissingObjectID)
			}

			if reply := op.call(mod, holdID(1)); !isAck(reply) {
				t.Fatalf("valid id: reply %T, want *astral.Ack", reply)
			}
		})
	}
}

func isAck(o astral.Object) bool {
	_, ok := o.(*astral.Ack)
	return ok
}

func TestModuleHoldObjectFailsClosedOnDBError(t *testing.T) {
	mod := testHoldModule(t)

	sqlDB, err := mod.db.DB.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	// note: nothing is held, so only the error path answers true.
	if !mod.HoldObject(holdID(1)) {
		t.Fatal("Module.HoldObject = false after a DB error, want true")
	}
}
