package apphost

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func storeObjectsGrant() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.StoreObjectsAction{}.ObjectType())}
}

func storeObjectsAction(actor *astral.Identity) *auth.StoreObjectsAction {
	return &auth.StoreObjectsAction{Action: auth.NewAction(actor)}
}

// TestStoreObjectsGrantAuthorizesTheGrantedApp is the node-local grant path: an
// app this node granted StoreObjects is authorized as itself, and an identity
// holding no grant is not.
//
// note: objects.register_blueprint authorizes this action, so this path decides
// whether a served app's own types reach a consumer.
func TestStoreObjectsGrantAuthorizesTheGrantedApp(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, storeObjectsGrant(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	action := storeObjectsAction(app)
	if !mod.AuthorizeStoreObjects(nil, action) {
		t.Fatal("the granted app was refused")
	}

	if !action.Actor().IsEqual(app) {
		t.Fatalf("the actor became %v; want the app %v", action.Actor(), app)
	}

	if mod.AuthorizeStoreObjects(nil, storeObjectsAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestStoreObjectsGrantRefusesUnusableGrants covers the grants that must not
// authorize: an expired one, a constrained one, and one for another action.
//
// note: a SeeObjects grant is the wrong-action case on purpose. Reading the
// objects a node holds never implies writing them.
func TestStoreObjectsGrantRefusesUnusableGrants(t *testing.T) {
	mod := testGrantModule(t)
	expired, constrained, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	past := time.Now().UTC().Add(-time.Hour)

	narrowed := storeObjectsGrant()
	narrowed.Constraints = astral.NewBundle()
	if err := narrowed.Constraints.Append(&astral.Ack{}); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	grants := []struct {
		id        *astral.Identity
		permit    *auth.Permit
		expiresAt *time.Time
	}{
		{expired, storeObjectsGrant(), &past},
		{constrained, narrowed, nil},
		{other, seeObjectsGrant(), nil},
	}
	for _, g := range grants {
		if err := mod.Grant(g.id, g.permit, g.expiresAt); err != nil {
			t.Fatalf("grant: %v", err)
		}
	}

	refused := map[string]*astral.Identity{
		"an expired grant":    expired,
		"a constrained grant": constrained,
		"a SeeObjects grant":  other,
	}
	for name, id := range refused {
		if mod.AuthorizeStoreObjects(nil, storeObjectsAction(id)) {
			t.Errorf("%s authorized StoreObjects", name)
		}
	}
}
