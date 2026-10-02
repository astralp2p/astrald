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

// TestServiceDiscoveryAllowsSwarmMembersOnThisNode: a current member may
// discover every service on this node, so a node can carry an app's discovery
// to the swarm. It holds nothing for another node, and expelled members and
// other users' nodes hold nothing.
func TestServiceDiscoveryAllowsSwarmMembersOnThisNode(t *testing.T) {
	nodeID, userID := astral.GenerateIdentity(), astral.GenerateIdentity()
	member, expelled := astral.GenerateIdentity(), astral.GenerateIdentity()
	outsider := astral.GenerateIdentity()

	mod := banModule(t, userID)
	mod.node = &identityNode{id: nodeID}
	mod.Deps.Auth = &adminNetworkContracts{contracts: []*auth.SignedContract{
		adminNetworkMembership(userID, nodeID),
		adminNetworkMembership(userID, member),
		adminNetworkMembership(userID, expelled),
		adminNetworkMembership(astral.GenerateIdentity(), outsider),
	}}
	if err := mod.db.StoreExpulsion(sampleSigned(userID, expelled)); err != nil {
		t.Fatalf("store expulsion: %v", err)
	}

	on := func(actor, node *astral.Identity) *services.ServiceDiscoveryAction {
		a := discoveryAction(actor, "player")
		a.NodeID = node
		return a
	}
	cases := []struct {
		name   string
		action *services.ServiceDiscoveryAction
		want   bool
	}{
		{"a member, on this node", on(member, nodeID), true},
		{"a member, on another node", on(member, astral.GenerateIdentity()), false},
		{"an expelled member", on(expelled, nodeID), false},
		{"another user's node", on(outsider, nodeID), false},
		{"this node itself", on(nodeID, nodeID), false},
	}
	for _, c := range cases {
		if got := mod.AuthorizeServiceDiscovery(nil, c.action); got != c.want {
			t.Errorf("%s: authorized %v; want %v", c.name, got, c.want)
		}
	}
}
