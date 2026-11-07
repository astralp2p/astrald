package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// TestInfoReportsStateBeforeTheClaim: a fresh or unclaimed node answers its
// state and manager, with empty contract fields, to any caller.
func TestInfoReportsStateBeforeTheClaim(t *testing.T) {
	manager := astral.GenerateIdentity()
	for state, m := range map[string]*astral.Identity{user.StateFresh: nil, user.StateUnclaimed: manager} {
		nodeID := astral.GenerateIdentity()
		mod := &Module{
			Deps: Deps{Auth: &recordingAuth{verdict: false}, Apphost: &tokenIssuer{state: state, manager: m}, Sys: &tokenIssuer{state: state, manager: m}},
			node: &identityNode{id: nodeID},
		}

		objs, err := routeOp(t, mod.OpInfo, query.New(astral.GenerateIdentity(), nodeID, "user.info", nil), "", nil)
		if err != nil {
			t.Fatalf("%v: route: %v", state, err)
		}
		if len(objs) != 1 {
			t.Fatalf("%v: answered %v; want one info", state, objs)
		}
		info, ok := objs[0].(*user.Info)
		if !ok {
			t.Fatalf("%v: answered %v; want mod.user.info", state, objs[0])
		}
		if string(info.State) != state || !info.Manager.IsEqual(m) || info.Contract != nil || info.ContractID != nil {
			t.Fatalf("%v: info %+v; want the state, the manager and no contract", state, info)
		}
	}
}

// TestInfoReportsStateAndManagerOnAClaimedNode: an authorized caller reads
// the contract, the state and the manager.
func TestInfoReportsStateAndManagerOnAClaimedNode(t *testing.T) {
	userID := astral.GenerateIdentity()
	manager := astral.GenerateIdentity()
	mod := swarmModule(t, &recordingAuth{verdict: true}, userID)
	mod.Apphost = &tokenIssuer{state: user.StateClaimed, manager: manager}
	mod.Sys = mod.Apphost.(*tokenIssuer)
	mod.Dir = namedResolvingDir{mod.Dir.(*resolvingDir)}

	objs, err := routeOp(t, mod.OpInfo, query.New(userID, astral.GenerateIdentity(), "user.info", nil), "", nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	info, ok := objs[0].(*user.Info)
	if !ok {
		t.Fatalf("answered %v; want mod.user.info", objs)
	}
	if info.State != user.StateClaimed || !info.Manager.IsEqual(manager) || info.Contract == nil {
		t.Fatalf("info %+v; want claimed, the manager and the contract", info)
	}
}

// namedResolvingDir is a resolvingDir that names every identity "named".
type namedResolvingDir struct{ *resolvingDir }

func (namedResolvingDir) DisplayName(*astral.Identity) string { return "named" }
