package apphost

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
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

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbGrant{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return &Module{db: db, log: log.New(nil)}
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

// TestGrantReplacesRatherThanAccumulates is why the table is unique on
// (identity, action): re-granting narrower roles has to take roles away.
// Contract permits union across the chain walk; a grant must not.
func TestGrantReplacesRatherThanAccumulates(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, servePermit(auth.RoleDescriber, auth.RoleFinder), nil); err != nil {
		t.Fatalf("first grant: %v", err)
	}
	if err := mod.Grant(app, servePermit(auth.RoleFinder), nil); err != nil {
		t.Fatalf("second grant: %v", err)
	}

	if mod.authorizeGrant(nil, serveAction(app, auth.RoleDescriber)) {
		t.Fatal("a narrowed grant still authorizes the role it dropped")
	}
	if !mod.authorizeGrant(nil, serveAction(app, auth.RoleFinder)) {
		t.Fatal("a narrowed grant lost the role it kept")
	}

	grants, err := mod.Grants(app)
	if err != nil {
		t.Fatalf("grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("re-granting left %d rows; want 1", len(grants))
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
