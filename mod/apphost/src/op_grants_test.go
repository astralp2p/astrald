package apphost

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// grantAdminOp is one row of the grant-administration surface in mod/apphost:
// the op and a query that binds its required arguments.
type grantAdminOp struct {
	name string
	op   func(*Module) any
	args string
}

func grantAdminOps() []grantAdminOp {
	id := astral.GenerateIdentity().String()
	action := auth.ServeObjectsAction{}.ObjectType()

	return []grantAdminOp{
		{"apphost.grant", func(m *Module) any { return m.OpGrant }, "?id=" + id + "&action=" + action},
		{"apphost.revoke", func(m *Module) any { return m.OpRevoke }, "?id=" + id + "&action=" + action},
		{"apphost.list_grants", func(m *Module) any { return m.OpListGrants }, "?id=" + id},
	}
}

// TestGrantAdminRefusesCallerWithoutPermits is the coverage measure for the
// grant-administration ops: each must ask before it reads or writes a grant,
// and must reject when the answer is no.
//
// The module is a bare struct — no database, no logger. An op that reached past
// its authorization check would panic on a nil field, so "touches no grant" is
// enforced by construction as well as by the byte count.
func TestGrantAdminRefusesCallerWithoutPermits(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range grantAdminOps() {
		t.Run(op.name, func(t *testing.T) {
			authority := &recordingAuth{verdict: false}
			mod := &Module{Deps: Deps{Auth: authority}}
			w := newRecordingWriter()

			err := route(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a caller holding no permits: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
			}

			checkAdminManageAppsAsked(t, op.name, authority, caller)
		})
	}
}

// TestGrantAdminRefusesQueryOffALink: a grant authorizes on this node alone, so
// every grant op refuses a query that arrived over a link whatever the
// authority answers.
func TestGrantAdminRefusesQueryOffALink(t *testing.T) {
	caller := astral.GenerateIdentity()

	for _, op := range grantAdminOps() {
		t.Run(op.name, func(t *testing.T) {
			mod := &Module{Deps: Deps{Auth: &recordingAuth{verdict: true}}}
			w := newRecordingWriter()

			err := routeAdminManageAppsFromNetwork(t, op.op(mod), caller, op.name+op.args, w)

			var rejected *astral.ErrRejected
			if !errors.As(err, &rejected) {
				t.Fatalf("%s answered a query off a link: got err %v, want a rejection", op.name, err)
			}

			if n := w.written(); n != 0 {
				t.Fatalf("%s wrote %d bytes to a query off a link; want none", op.name, n)
			}
		})
	}
}

// TestGrantOpRecordsForAnExistingIdentity is the point of the op: an identity
// that already exists gains an action without re-registering, and the grant the
// node records is the action whole, non-delegable.
func TestGrantOpRecordsForAnExistingIdentity(t *testing.T) {
	mod := testGrantModule(t)
	authority := &recordingAuth{verdict: true}
	mod.Auth = authority
	caller, app := astral.GenerateIdentity(), astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	w := newRecordingWriter()
	err := route(t, mod.OpGrant, caller, "apphost.grant?id="+app.String()+"&action="+action, w)
	if err != nil {
		t.Fatalf("apphost.grant refused an authorized caller: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("the node holds %d grants for the app; want 1", len(grants))
	}
	if string(grants[0].Action) != action {
		t.Fatalf("recorded action %q; want %q", grants[0].Action, action)
	}
	if grants[0].Delegation != 0 {
		t.Fatalf("recorded delegation %d; want 0", grants[0].Delegation)
	}

	checkAdminManageAppsAsked(t, "apphost.grant", authority, caller)
}

// TestRevokeOpWithdrawsTheGrant: the op removes the row, so the identity stops
// holding the action.
func TestRevokeOpWithdrawsTheGrant(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	if err := mod.Grant(app, &auth.Permit{Action: astral.String8(action)}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	w := newRecordingWriter()
	err := route(t, mod.OpRevoke, astral.GenerateIdentity(), "apphost.revoke?id="+app.String()+"&action="+action, w)
	if err != nil {
		t.Fatalf("apphost.revoke refused an authorized caller: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 0 {
		t.Fatalf("the node still holds %d grants after a revoke; want 0", len(grants))
	}
}

// TestListGrantsOmitsAnExpiredGrant: the wire listing answers what authorizes
// now. The row survives for the Go-side Grants, which reports what was granted.
func TestListGrantsOmitsAnExpiredGrant(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()
	live := auth.ServeObjectsAction{}.ObjectType()
	lapsed := auth.ServeAppsAction{}.ObjectType()
	past := time.Now().UTC().Add(-time.Hour)

	if err := mod.Grant(app, &auth.Permit{Action: astral.String8(live)}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := mod.Grant(app, &auth.Permit{Action: astral.String8(lapsed)}, &past); err != nil {
		t.Fatalf("grant: %v", err)
	}

	active, err := mod.activeGrants(app)
	if err != nil {
		t.Fatalf("active grants: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("the listing reported %d grants; want the 1 that still authorizes", len(active))
	}
	if string(active[0].Action) != live {
		t.Fatalf("the listing reported %q; want %q", active[0].Action, live)
	}

	all, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the expired grant vanished from the record: %d rows; want 2", len(all))
	}
}

// TestGrantOpBoundsTheGrantByDuration: a duration expires the grant, and the
// listing keeps it until it lapses.
func TestGrantOpBoundsTheGrantByDuration(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	w := newRecordingWriter()
	err := route(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant?id="+app.String()+"&action="+action+"&duration=1h", w)
	if err != nil {
		t.Fatalf("apphost.grant refused an authorized caller: %v", err)
	}
	waitAdminManageAppsAnswer(t, w)

	rows, err := mod.db.ListGrants(app)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the node holds %d grants; want 1", len(rows))
	}
	if rows[0].ExpiresAt == nil {
		t.Fatal("a grant asked to last an hour was recorded without an expiry")
	}

	active, err := mod.activeGrants(app)
	if err != nil {
		t.Fatalf("active grants: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("a grant with an hour to run is listed %d times; want 1", len(active))
	}
}

// TestGrantOpsAreReachableByName pins the names the operator types. The module
// registers its ops by reflecting over Op-prefixed methods, so a renamed method
// renames the operation, and the tests above would keep passing because they
// call the method rather than route to its name.
//
// note: the registration error is discarded the way the loader discards it, so
// this test measures reachability and nothing else.
func TestGrantOpsAreReachableByName(t *testing.T) {
	var router routing.OpRouter
	_ = router.AddStructPrefix(&Module{}, "Op")

	for _, name := range []string{"grant", "revoke", "list_grants"} {
		if !router.HasRoute(name) {
			t.Fatalf("apphost.%s is not routable", name)
		}
	}
}
