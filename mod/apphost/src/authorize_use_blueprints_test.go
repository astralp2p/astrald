package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func useBlueprintsGrant() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())}
}

// useBlueprintsAction is the shape every blueprint op authorizes with
// (mod/objects/src/op_blueprints.go).
func useBlueprintsAction(actor *astral.Identity) *auth.UseBlueprintsAction {
	return &auth.UseBlueprintsAction{Action: auth.NewAction(actor)}
}

// TestUseBlueprintsGrantAuthorizesTheGrantedApp is the node-local grant path: an app
// this node granted UseBlueprints is authorized as itself, and an identity holding
// no grant is not.
func TestUseBlueprintsGrantAuthorizesTheGrantedApp(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, useBlueprintsGrant(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	action := useBlueprintsAction(app)
	if !mod.AuthorizeUseBlueprints(nil, action) {
		t.Fatal("the granted app was refused")
	}

	if !action.Actor().IsEqual(app) {
		t.Fatalf("the actor became %v; want the app %v", action.Actor(), app)
	}

	if mod.AuthorizeUseBlueprints(nil, useBlueprintsAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestUseBlueprintsGrantRefusesUnusableGrants covers the grants that must not
// authorize: an expired one, a constrained one, and one for another action.
func TestUseBlueprintsGrantRefusesUnusableGrants(t *testing.T) {
	mod := testGrantModule(t)
	expired, constrained, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	// note: FindGrant compares a stored expiry with the UTC clock (db_grants.go).
	past := time.Now().UTC().Add(-time.Hour)

	narrowed := useBlueprintsGrant()
	narrowed.Constraints = astral.NewBundle()
	if err := narrowed.Constraints.Append(&astral.Ack{}); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	grants := []struct {
		id        *astral.Identity
		permit    *auth.Permit
		expiresAt *time.Time
	}{
		{expired, useBlueprintsGrant(), &past},
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
		if mod.AuthorizeUseBlueprints(nil, useBlueprintsAction(id)) {
			t.Errorf("%s authorized UseBlueprints", name)
		}
	}
}
