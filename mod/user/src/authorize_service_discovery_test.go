package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
)

func discoveryAction(actor *astral.Identity, service astral.String8) *services.ServiceDiscoveryAction {
	return &services.ServiceDiscoveryAction{Action: auth.NewAction(actor), Service: service}
}

// TestServiceDiscoveryAllowsTheUser covers the one default holder: the user,
// for every service.
func TestServiceDiscoveryAllowsTheUser(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := claimedModule(t, userID)
	mod.node = &identityNode{id: astral.GenerateIdentity()}

	for _, service := range []astral.String8{"player", "nat", "contacts-backend"} {
		if !mod.AuthorizeServiceDiscovery(nil, discoveryAction(userID, service)) {
			t.Fatalf("the user must discover %q by default", service)
		}
	}
}

// TestServiceDiscoveryRefusesEveryoneElse: this node, a stranger and a zero
// actor hold nothing by default. A local caller carrying no identity arrives as
// this node (core/router.go).
func TestServiceDiscoveryRefusesEveryoneElse(t *testing.T) {
	nodeID := astral.GenerateIdentity()
	mod := claimedModule(t, astral.GenerateIdentity())
	mod.node = &identityNode{id: nodeID}

	for name, actor := range map[string]*astral.Identity{
		"this node": nodeID,
		"stranger":  astral.GenerateIdentity(),
		"zero":      {},
		"nil":       nil,
	} {
		if mod.AuthorizeServiceDiscovery(nil, discoveryAction(actor, "player")) {
			t.Fatalf("%s must not discover by default", name)
		}
	}
}
