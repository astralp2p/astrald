package user

import (
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// contractOpModule is node nodeID, linked to the linked identities, holding
// active as its active contract unless active is nil.
func contractOpModule(t *testing.T, nodeID *astral.Identity, active *auth.SignedContract, linked ...*astral.Identity) *Module {
	t.Helper()

	nodes := newLinkNodes()
	for _, id := range linked {
		nodes.setLinked(id, true)
	}
	mod := &Module{Deps: Deps{Nodes: nodes}, node: &identityNode{id: nodeID}}
	if active != nil {
		if err := mod.config.ActiveContract.Set(nil, active); err != nil {
			t.Fatalf("seed active contract: %v", err)
		}
	}
	return mod
}

// askContract sends user.contract from caller to mod's node and returns the answer.
func askContract(t *testing.T, mod *Module, caller *astral.Identity) []astral.Object {
	t.Helper()

	objs, err := routeOp(t, mod.OpContract, query.New(caller, mod.node.Identity(), user.OpContract, nil), "", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("answered %v objects; want one", len(objs))
	}
	return objs
}

// TestContractAnswersALinkedCaller: a caller with a direct link reads the
// node's active contract.
func TestContractAnswersALinkedCaller(t *testing.T) {
	nodeID, caller := astral.GenerateIdentity(), astral.GenerateIdentity()
	active := nodeContract(astral.GenerateIdentity(), nodeID)
	mod := contractOpModule(t, nodeID, active, caller)

	got, ok := askContract(t, mod, caller)[0].(*auth.SignedContract)
	if !ok {
		t.Fatal("answered no signed contract")
	}
	gotID, err := astral.ResolveObjectID(got)
	if err != nil {
		t.Fatal(err)
	}
	wantID, err := astral.ResolveObjectID(active)
	if err != nil {
		t.Fatal(err)
	}
	if !gotID.IsEqual(wantID) {
		t.Fatalf("answered contract %v; want the active contract %v", gotID, wantID)
	}
}

// TestContractRefuses: a caller without a direct link is refused first, also on
// a node without a contract; a linked caller of a node without a contract is
// answered that there is none.
func TestContractRefuses(t *testing.T) {
	nodeID, caller := astral.GenerateIdentity(), astral.GenerateIdentity()
	active := nodeContract(astral.GenerateIdentity(), nodeID)

	cases := []struct {
		name string
		mod  *Module
		want astral.Error
	}{
		{"not linked", contractOpModule(t, nodeID, active), auth.ErrNotPermitted},
		{"another node linked", contractOpModule(t, nodeID, active, astral.GenerateIdentity()), auth.ErrNotPermitted},
		{"not linked and no contract", contractOpModule(t, nodeID, nil), auth.ErrNotPermitted},
		{"linked and no contract", contractOpModule(t, nodeID, nil, caller), user.ErrNoActiveContract},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := askContract(t, tt.mod, caller)[0].(astral.Error)
			if !ok || got.Error() != tt.want.Error() {
				t.Fatalf("answered %v; want %q", got, tt.want)
			}
		})
	}
}

// TestContractIsMounted: the module serves the op under the name the astral-go
// client queries, inside the module's scope.
func TestContractIsMounted(t *testing.T) {
	mod := &Module{}
	if err := mod.router.AddStructPrefix(mod, "Op"); err != nil {
		t.Fatalf("mount: %v", err)
	}

	name, ok := strings.CutPrefix(user.OpContract, usermod.ModuleName+".")
	if !ok || !mod.router.HasRoute(name) {
		t.Fatalf("no route %q in scope %q for %q", name, usermod.ModuleName, user.OpContract)
	}
}
