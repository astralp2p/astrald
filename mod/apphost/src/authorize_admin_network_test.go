package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func adminNetworkGrant() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.AdminNetworkAction{}.ObjectType())}
}

func adminNetworkAction(actor *astral.Identity) *auth.AdminNetworkAction {
	return &auth.AdminNetworkAction{Action: auth.NewAction(actor)}
}

// TestAdminNetworkGrantAuthorizesTheGrantedApp is the node-local grant path: an
// app this node granted AdminNetwork is authorized as itself, and an identity
// holding no grant is not.
func TestAdminNetworkGrantAuthorizesTheGrantedApp(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, adminNetworkGrant(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	action := adminNetworkAction(app)
	if !mod.AuthorizeAdminNetwork(nil, action) {
		t.Fatal("the granted app was refused")
	}

	if !action.Actor().IsEqual(app) {
		t.Fatalf("the actor became %v; want the app %v", action.Actor(), app)
	}

	if mod.AuthorizeAdminNetwork(nil, adminNetworkAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestAdminNetworkGrantRefusesUnusableGrants covers the grants that must not
// authorize: an expired one, a constrained one, and one for another action.
func TestAdminNetworkGrantRefusesUnusableGrants(t *testing.T) {
	mod := testGrantModule(t)
	expired, constrained, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	// note: FindGrant compares a stored expiry with the UTC clock (db_grants.go).
	// note: an expiry written in local time east of UTC outlives its instant there.
	past := time.Now().UTC().Add(-time.Hour)

	narrowed := adminNetworkGrant()
	narrowed.Constraints = astral.NewBundle()
	if err := narrowed.Constraints.Append(&astral.Ack{}); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	grants := []struct {
		id        *astral.Identity
		permit    *auth.Permit
		expiresAt *time.Time
	}{
		{expired, adminNetworkGrant(), &past},
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
		if mod.AuthorizeAdminNetwork(nil, adminNetworkAction(id)) {
			t.Errorf("%s authorized AdminNetwork", name)
		}
	}
}
