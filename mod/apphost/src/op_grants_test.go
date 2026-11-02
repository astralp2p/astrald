package apphost

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
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
		{"apphost.grant", func(m *Module) any { return m.OpGrant }, ""},
		{"apphost.revoke", func(m *Module) any { return m.OpRevoke }, "?identity=" + id + "&action=" + action},
		{"apphost.list_grants", func(m *Module) any { return m.OpListGrants }, "?identity=" + id},
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

			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := w.refusal(t), auth.ErrNotPermitted.Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
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

			if err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := w.refusal(t), astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
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
	req := &apphost.GrantRequest{Identity: app, Permits: []*auth.Permit{{Action: astral.String8(action), Delegation: 3}}}
	if err := routeSending(t, mod.OpGrant, caller, "apphost.grant", req, w); err != nil {
		t.Fatalf("apphost.grant refused an authorized caller: %v", err)
	}
	if got := readReply(t, w); got != ackAnswer {
		t.Fatalf("answered %q; want ack", got)
	}

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
	err := route(t, mod.OpRevoke, astral.GenerateIdentity(), "apphost.revoke?identity="+app.String()+"&action="+action, w)
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

// TestRevokeOpByIDKeepsTheSibling: revoke by id withdraws that grant alone,
// and the identity keeps its other grant for the same action.
func TestRevokeOpByIDKeepsTheSibling(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app := astral.GenerateIdentity()

	for _, p := range []*auth.Permit{discoveryPermit("player"), discoveryPermit("wallet")} {
		if err := mod.Grant(app, p, nil); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	rows, err := mod.db.ListGrants(app)
	if err != nil || len(rows) != 2 {
		t.Fatalf("seeded %d rows (%v); want 2", len(rows), err)
	}

	w := newRecordingWriter()
	q := fmt.Sprintf("apphost.revoke?identity=%v&id=%v", app, rows[0].ID)
	if err := route(t, mod.OpRevoke, astral.GenerateIdentity(), q, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got := readReply(t, w); got != ackAnswer {
		t.Fatalf("answered %q; want ack", got)
	}

	left, err := mod.db.ListGrants(app)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(left) != 1 || left[0].ID != rows[1].ID {
		t.Fatalf("rows left %+v; want the sibling %v alone", left, rows[1].ID)
	}
}

// TestRevokeOpNeedsExactlyOneOfIDOrAction: both or neither is an error, and
// nothing is withdrawn.
func TestRevokeOpNeedsExactlyOneOfIDOrAction(t *testing.T) {
	action := auth.ServeObjectsAction{}.ObjectType()

	for args, want := range map[string]string{
		"&id=1&action=" + action: "give id or action, not both",
		"":                       "missing id or action",
	} {
		t.Run(want, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}
			app := astral.GenerateIdentity()
			if err := mod.Grant(app, &auth.Permit{Action: astral.String8(action)}, nil); err != nil {
				t.Fatalf("grant: %v", err)
			}

			w := newRecordingWriter()
			if err := route(t, mod.OpRevoke, astral.GenerateIdentity(), "apphost.revoke?identity="+app.String()+args, w); err != nil {
				t.Fatalf("route: %v", err)
			}
			if got := readReply(t, w); got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}
			if held := heldActions(t, mod, app); len(held) != 1 {
				t.Fatalf("the identity holds %q; want its grant kept", held)
			}
		})
	}
}

// TestRevokeOpIDOfAnotherIdentityIsNotFound: an ID names a grant of one
// identity, so naming it for another withdraws nothing.
func TestRevokeOpIDOfAnotherIdentityIsNotFound(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	app, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := mod.Grant(app, &auth.Permit{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	rows, err := mod.db.ListGrants(app)
	if err != nil || len(rows) != 1 {
		t.Fatalf("seeded %d rows (%v); want 1", len(rows), err)
	}

	w := newRecordingWriter()
	q := fmt.Sprintf("apphost.revoke?identity=%v&id=%v", other, rows[0].ID)
	if err := route(t, mod.OpRevoke, astral.GenerateIdentity(), q, w); err != nil {
		t.Fatalf("route: %v", err)
	}
	if got, want := readReply(t, w), "grant not found"; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if held := heldActions(t, mod, app); len(held) != 1 {
		t.Fatalf("the owner holds %q; want its grant kept", held)
	}
}

// TestListGrantsEmitsIDAndExpiry: the op emits one mod.apphost.grant per
// unexpired row with the row's ID and expiry, nil for a grant until revoked,
// then eos.
func TestListGrantsEmitsIDAndExpiry(t *testing.T) {
	mod := testGrantModule(t)
	mod.Auth = &recordingAuth{verdict: true}
	mod.Dir = &namingDir{}
	app := astral.GenerateIdentity()
	soon := time.Now().UTC().Add(time.Hour)
	past := time.Now().UTC().Add(-time.Hour)

	seed := []struct {
		action    string
		expiresAt *time.Time
	}{
		{auth.ServeObjectsAction{}.ObjectType(), nil},
		{auth.SeeObjectsAction{}.ObjectType(), &soon},
		{auth.ServeAppsAction{}.ObjectType(), &past},
	}
	for _, g := range seed {
		if err := mod.Grant(app, &auth.Permit{Action: astral.String8(g.action)}, g.expiresAt); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	rows, err := mod.db.ListGrants(app)
	if err != nil || len(rows) != 3 {
		t.Fatalf("seeded %d rows (%v); want 3", len(rows), err)
	}

	r, w := io.Pipe()
	defer r.Close()
	if err := route(t, mod.OpListGrants, astral.GenerateIdentity(), "apphost.list_grants?identity="+app.String(), w); err != nil {
		t.Fatalf("route: %v", err)
	}

	var grants []*apphost.Grant
	in := struct {
		io.Reader
		io.Writer
	}{r, io.Discard}
	err = channel.New(in).Switch(channel.Collect[*apphost.Grant](&grants), channel.BreakOnEOS, channel.FailOnEOF())
	if err != nil {
		t.Fatalf("read listing: %v", err)
	}

	if len(grants) != 2 {
		t.Fatalf("listed %d grants; want the 2 unexpired", len(grants))
	}
	if uint(grants[0].ID) != rows[0].ID || string(grants[0].Permit.Action) != seed[0].action || grants[0].ExpiresAt != nil {
		t.Fatalf("first grant %+v; want ID %v, %v, until revoked", grants[0], rows[0].ID, seed[0].action)
	}
	if uint(grants[1].ID) != rows[1].ID || grants[1].ExpiresAt == nil || !time.Time(*grants[1].ExpiresAt).Equal(soon) {
		t.Fatalf("second grant %+v; want ID %v expiring at %v", grants[1], rows[1].ID, soon)
	}
}

// TestGrantOpRefusesRemovedArguments: each argument the grant_request object
// replaced answers unknown argument, and nothing is written.
func TestGrantOpRefusesRemovedArguments(t *testing.T) {
	for _, arg := range []string{"identity", "action", "duration", "constrained"} {
		t.Run(arg, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}
			app := astral.GenerateIdentity()

			w := newRecordingWriter()
			req := &apphost.GrantRequest{Identity: app, Permits: permits(auth.ServeObjectsAction{}.ObjectType())}
			if err := routeSending(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant?"+arg+"=x", req, w); err != nil {
				t.Fatalf("route: %v", err)
			}
			if got, want := readReply(t, w), "unknown argument: "+arg; got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}
			if held := heldActions(t, mod, app); len(held) != 0 {
				t.Fatalf("a refused grant wrote %q", held)
			}
		})
	}
}

// TestGrantOpRefusesBeforeWriting: a request without permits, with a negative
// duration, or with an invalid permit is answered with an error and writes
// nothing, not even its valid permits.
func TestGrantOpRefusesBeforeWriting(t *testing.T) {
	serveObjects := auth.ServeObjectsAction{}.ObjectType()

	for _, tc := range []struct {
		name string
		req  *apphost.GrantRequest
		want string
	}{
		{"no permits", &apphost.GrantRequest{}, "missing permits"},
		{"negative duration", &apphost.GrantRequest{Duration: -1, Permits: permits(serveObjects)}, "negative duration"},
		{"unknown action", &apphost.GrantRequest{Permits: permits(serveObjects, "mod.nope.no_action")}, "invalid permit: mod.nope.no_action: unknown action"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mod := testGrantModule(t)
			mod.Auth = &recordingAuth{verdict: true}
			app := astral.GenerateIdentity()
			tc.req.Identity = app

			w := newRecordingWriter()
			if err := routeSending(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", tc.req, w); err != nil {
				t.Fatalf("route: %v", err)
			}
			if got := readReply(t, w); got != tc.want {
				t.Fatalf("answered %q; want %q", got, tc.want)
			}
			if held := heldActions(t, mod, app); len(held) != 0 {
				t.Fatalf("a refused grant wrote %q", held)
			}
		})
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
	if string(active[0].Permit.Action) != live {
		t.Fatalf("the listing reported %q; want %q", active[0].Permit.Action, live)
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
	req := &apphost.GrantRequest{Identity: app, Duration: astral.Duration(time.Hour), Permits: permits(action)}
	if err := routeSending(t, mod.OpGrant, astral.GenerateIdentity(), "apphost.grant", req, w); err != nil {
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
