package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func seeObjectsGrant() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.SeeObjectsAction{}.ObjectType())}
}

// seeObjectsAction names no object and no repository, which is the shape
// objects.blueprints authorizes with (mod/objects/src/op_blueprints.go).
func seeObjectsAction(actor *astral.Identity) *auth.SeeObjectsAction {
	return &auth.SeeObjectsAction{Action: auth.NewAction(actor)}
}

// TestSeeObjectsGrantAuthorizesTheGrantedApp is the node-local grant path: an app
// this node granted SeeObjects is authorized as itself, and an identity holding
// no grant is not.
//
// note: a served app syncs its blueprints during apps.Serve, so this path is what
// decides whether a registered app starts at all.
func TestSeeObjectsGrantAuthorizesTheGrantedApp(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, seeObjectsGrant(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	action := seeObjectsAction(app)
	if !mod.AuthorizeSeeObjects(nil, action) {
		t.Fatal("the granted app was refused")
	}

	if !action.Actor().IsEqual(app) {
		t.Fatalf("the actor became %v; want the app %v", action.Actor(), app)
	}

	if mod.AuthorizeSeeObjects(nil, seeObjectsAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestSeeObjectsGrantRefusesUnusableGrants covers the grants that must not
// authorize: an expired one, a constrained one, and one for another action.
func TestSeeObjectsGrantRefusesUnusableGrants(t *testing.T) {
	mod := testGrantModule(t)
	expired, constrained, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	// note: FindGrant compares a stored expiry with the UTC clock (db_grants.go).
	past := time.Now().UTC().Add(-time.Hour)

	narrowed := seeObjectsGrant()
	narrowed.Constraints = astral.NewBundle()
	if err := narrowed.Constraints.Append(&astral.Ack{}); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	grants := []struct {
		id        *astral.Identity
		permit    *auth.Permit
		expiresAt *time.Time
	}{
		{expired, seeObjectsGrant(), &past},
		{constrained, narrowed, nil},
		{other, &auth.Permit{Action: astral.String8(auth.ServeObjectsAction{}.ObjectType())}, nil},
	}
	for _, g := range grants {
		if err := mod.Grant(g.id, g.permit, g.expiresAt); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}

	refused := map[string]*astral.Identity{
		"an expired grant":           expired,
		"a constrained grant":        constrained,
		"a grant for another action": other,
	}
	for name, id := range refused {
		if mod.AuthorizeSeeObjects(nil, seeObjectsAction(id)) {
			t.Errorf("%s authorized SeeObjects", name)
		}
	}
}
