package apphost

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testGrantModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// note: every connection to :memory: opens its own database, so the pool
	// holds one.
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	db := &DB{DB: gdb}
	if err := db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{Deps: Deps{Dir: &namingDir{}}, db: db, log: log.New(nil)}
}

func servePermit(roles ...astral.String8) *auth.Permit {
	permit := &auth.Permit{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}

	if len(roles) > 0 {
		permit.Constraints = astral.NewBundle()
		for _, role := range roles {
			if err := permit.Constraints.Append(astral.NewString8(string(role))); err != nil {
				panic(err)
			}
		}
	}

	return permit
}

func serveAction(actor *astral.Identity, role astral.String8) *auth.ServeObjectsAction {
	return &auth.ServeObjectsAction{Action: auth.NewAction(actor), Role: role}
}

// TestGrantAuthorizesOnlyGrantedRoles is the point of the whole mechanism: one
// grant lets an app describe without letting it search, and the authorizer
// learns that from the permit rather than from anything role-specific.
func TestGrantAuthorizesOnlyGrantedRoles(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, servePermit(auth.RoleDescriber), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if !mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("granted role was refused")
	}
	if mod.authorizeGrant(nil, serveAction(app, auth.RoleSearcher)) {
		t.Fatal("ungranted role was allowed")
	}
}

func TestGrantRefusesUngrantedIdentity(t *testing.T) {
	mod := testGrantModule(t)

	if err := mod.Grant(astral.GenerateIdentity(), servePermit(auth.RoleDescriber), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	stranger := astral.GenerateIdentity()
	if mod.authorizeGrant(nil, serveAction(stranger, auth.RoleDescriber)) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestGrantUnconstrainedCoversEveryRole covers the other end of the permit: a
// grant with no constraints is the whole action.
func TestGrantUnconstrainedCoversEveryRole(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, servePermit(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder, auth.RoleSearcher} {
		if !mod.authorizeGrant(nil, serveAction(app, role)) {
			t.Fatalf("unconstrained grant refused role %q", role)
		}
	}
}

// TestGrantsAccumulate: a second, narrower grant for the same action is added
// beside the first, and each role either grant names still authorizes.
// Narrowing is revoking the wide grant, not re-granting over it.
func TestGrantsAccumulate(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, servePermit(auth.RoleDescriber, auth.RoleFinder), nil); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := mod.Grant(app, servePermit(auth.RoleFinder), nil); err != nil {
		t.Fatalf("second grant: %v", err)
	}

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 2 {
		t.Fatalf("two different permits left %d rows; want 2", len(grants))
	}

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder} {
		if !mod.authorizeGrant(nil, serveAction(app, role)) {
			t.Fatalf("role %q is refused while a grant names it", role)
		}
	}
	if mod.authorizeGrant(nil, serveAction(app, auth.RoleSearcher)) {
		t.Fatal("a role no grant names was allowed")
	}
}

// TestIdenticalPermitIsStoredOnceWithTheNewerExpiry: re-granting the same
// permit moves its expiry and adds no row; a nil expiry makes it unexpiring.
func TestIdenticalPermitIsStoredOnceWithTheNewerExpiry(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	first := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	second := first.Add(time.Hour)
	for _, expiresAt := range []*time.Time{&first, &second} {
		if err := mod.Grant(app, servePermit(auth.RoleDescriber), expiresAt); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}

	rows, err := mod.db.ListGrants(app)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ExpiresAt == nil || !rows[0].ExpiresAt.Equal(second) {
		t.Fatalf("rows %+v; want one row expiring at %v", rows, second)
	}

	if err := mod.Grant(app, servePermit(auth.RoleDescriber), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	rows, err = mod.db.ListGrants(app)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].ExpiresAt != nil {
		t.Fatalf("rows %+v; want one row that does not expire", rows)
	}
}

// grantIDs returns the IDs of identity's grant rows, oldest first.
func grantIDs(t *testing.T, mod *Module, identity *astral.Identity) (ids []uint64) {
	t.Helper()

	rows, err := mod.db.ListGrants(identity)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, row := range rows {
		ids = append(ids, uint64(row.ID))
	}
	return ids
}

func TestRevokeGrantByIDKeepsTheSibling(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder} {
		if err := mod.Grant(app, servePermit(role), nil); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	ids := grantIDs(t, mod, app)
	if len(ids) != 2 {
		t.Fatalf("got %d rows; want 2", len(ids))
	}

	if err := mod.RevokeGrant(app, ids[0]); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("the revoked grant still authorizes")
	}
	if !mod.authorizeGrant(nil, serveAction(app, auth.RoleFinder)) {
		t.Fatal("the sibling grant was withdrawn with it")
	}
	if err := mod.RevokeGrant(app, ids[0]); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoking an absent ID: got %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestRevokeByActionClearsEveryRow(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder} {
		if err := mod.Grant(app, servePermit(role), nil); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}

	if err := mod.Revoke(app, auth.ServeObjectsAction{}.ObjectType()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if ids := grantIDs(t, mod, app); len(ids) != 0 {
		t.Fatalf("revoking the action left rows %v", ids)
	}
}

func TestRevokeGrantByIDOfAnotherIdentityDeletesNothing(t *testing.T) {
	mod := testGrantModule(t)
	app, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := mod.Grant(app, servePermit(auth.RoleDescriber), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ids := grantIDs(t, mod, app)

	if err := mod.RevokeGrant(other, ids[0]); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoking another identity's ID: got %v, want gorm.ErrRecordNotFound", err)
	}
	if !mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("another identity's revoke withdrew the grant")
	}
}

// TestGrantIDIsNotReused: a revoke names one row by its ID, so the ID of a
// revoked grant never comes back for a newer one.
func TestGrantIDIsNotReused(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	for _, role := range []astral.String8{auth.RoleDescriber, auth.RoleFinder, auth.RoleSearcher} {
		if err := mod.Grant(app, servePermit(role), nil); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}
	ids := grantIDs(t, mod, app)
	revoked := ids[len(ids)-1]
	if err := mod.RevokeGrant(app, revoked); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if err := mod.Grant(app, servePermit(auth.RoleDescriber, auth.RoleSearcher), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	ids = grantIDs(t, mod, app)
	if newest := ids[len(ids)-1]; newest <= revoked {
		t.Fatalf("the new grant got ID %v; want above the revoked %v", newest, revoked)
	}
}

// TestPermitEncodingIsStable: an identical permit is found by its bytes, so a
// decoded permit must encode to the bytes it came from.
func TestPermitEncodingIsStable(t *testing.T) {
	permit := &auth.Permit{
		Action:      astral.String8(services.ServiceDiscoveryAction{}.ObjectType()),
		Constraints: playerScope(),
	}

	row, err := fromGrantPermit(astral.GenerateIdentity(), permit, nil)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := toGrantPermit(row)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	again, err := fromGrantPermit(row.Identity, decoded, nil)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}

	if !bytes.Equal(row.Data, again.Data) {
		t.Fatalf("re-encoding changed the bytes:\n%x\n%x", row.Data, again.Data)
	}
}

func TestRevokeWithdrawsTheGrant(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()
	action := auth.ServeObjectsAction{}.ObjectType()

	if err := mod.Grant(app, servePermit(auth.RoleDescriber), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}
	if err := mod.Revoke(app, action); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("a revoked grant still authorizes")
	}

	if err := mod.Revoke(app, action); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoking an absent grant: got %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestExpiredGrantDoesNotAuthorize(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	past := time.Now().UTC().Add(-time.Hour)
	if err := mod.Grant(app, servePermit(auth.RoleDescriber), &past); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("an expired grant still authorizes")
	}

	// still listable — the operator can see what lapsed
	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("expired grant vanished from the listing: %d rows", len(grants))
	}
}

// TestGrantExpiryIgnoresLocalZone: the sqlite driver compares datetimes as
// text, so an expiry written in a local zone and queried against a UTC clock
// shifts by the zone's offset. East of UTC a lapsed grant would keep
// authorizing; west of UTC a live one would be refused early.
func TestGrantExpiryIgnoresLocalZone(t *testing.T) {
	local := time.Local
	t.Cleanup(func() { time.Local = local })

	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	time.Local = time.FixedZone("UTC+2", 2*60*60)
	past := time.Now().Add(-time.Hour)
	if err := mod.Grant(app, servePermit(auth.RoleDescriber), &past); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("an expired grant still authorizes on a host east of UTC")
	}

	time.Local = time.FixedZone("UTC-2", -2*60*60)
	future := time.Now().Add(time.Hour)
	if err := mod.Grant(app, servePermit(auth.RoleDescriber), &future); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if !mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("an unexpired grant is refused on a host west of UTC")
	}
}

// TestGrantForcesDelegationToZero: a grant is not portable evidence, so a hop
// count on it would describe authority it cannot carry.
func TestGrantForcesDelegationToZero(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	permit := servePermit(auth.RoleDescriber)
	permit.Delegation = 3

	if err := mod.Grant(app, permit, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("got %d grants, want 1", len(grants))
	}
	if grants[0].Delegation != 0 {
		t.Fatalf("stored delegation %d; want 0", grants[0].Delegation)
	}
}

func TestGrantRejectsIncompleteInput(t *testing.T) {
	mod := testGrantModule(t)

	if err := mod.Grant(nil, servePermit(auth.RoleDescriber), nil); !errors.Is(err, apphostmod.ErrInvalidIdentity) {
		t.Fatalf("nil identity: got %v", err)
	}
	if err := mod.Grant(astral.GenerateIdentity(), &auth.Permit{}, nil); !errors.Is(err, apphostmod.ErrInvalidPermit) {
		t.Fatalf("permit naming no action: got %v", err)
	}
}
