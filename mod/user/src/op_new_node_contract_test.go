package user

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// newNodeContractModule is a node claimed by userID whose directory resolves every
// name to named and whose auth module answers authority. The node has its own
// identity, the subject of a contract built without node.
func newNodeContractModule(t *testing.T, authority *recordingAuth, userID, named *astral.Identity) *Module {
	t.Helper()

	mod := claimedModule(t, userID)
	mod.Deps = Deps{Auth: authority, Dir: &resolvingDir{id: named}}
	mod.node = &identityNode{id: astral.GenerateIdentity()}
	return mod
}

// askNewNodeContract routes queryString to user.new_node_contract as caller with
// origin and returns the objects it answered.
func askNewNodeContract(t *testing.T, mod *Module, caller *astral.Identity, origin, queryString string) []astral.Object {
	t.Helper()

	objs, err := routeOp(t, mod.OpNewNodeContract, query.New(caller, caller, queryString, nil), origin, nil)
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("user.new_node_contract answered %v; want one object", objs)
	}
	return objs
}

// TestNewNodeContractRefusesNetworkOrigin: a network caller is answered origin not
// allowed before any lookup or authorization, with the user named or defaulted.
func TestNewNodeContractRefusesNetworkOrigin(t *testing.T) {
	for _, args := range []string{"?user=anyone", ""} {
		t.Run("args="+args, func(t *testing.T) {
			authority := &recordingAuth{verdict: true}
			mod := newNodeContractModule(t, authority, astral.GenerateIdentity(), astral.GenerateIdentity())

			objs := askNewNodeContract(t, mod, astral.GenerateIdentity(), astral.OriginNetwork, "user.new_node_contract"+args)

			msg, ok := objs[0].(*astral.ErrorMessage)
			if !ok {
				t.Fatalf("answered %v; want an error_message", objs[0])
			}
			if got, want := msg.Error(), astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error(); got != want {
				t.Fatalf("answered %q; want %q", got, want)
			}
			if n := len(authority.recorded()); n != 0 {
				t.Fatalf("made %d authorization calls; want none", n)
			}
		})
	}
}

// TestNewNodeContractDefaultUserNeedsSeeSwarm: with user omitted on a claimed node,
// the op asks SeeSwarm for the caller and a refusal answers not permitted.
func TestNewNodeContractDefaultUserNeedsSeeSwarm(t *testing.T) {
	authority := &recordingAuth{verdict: false}
	caller := astral.GenerateIdentity()
	mod := newNodeContractModule(t, authority, astral.GenerateIdentity(), astral.GenerateIdentity())

	objs := askNewNodeContract(t, mod, caller, "", "user.new_node_contract")

	msg, ok := objs[0].(*astral.ErrorMessage)
	if !ok {
		t.Fatalf("answered %v; want an error_message", objs[0])
	}
	if got, want := msg.Error(), auth.ErrNotPermitted.Error(); got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}

	actions := authority.recorded()
	if len(actions) != 1 {
		t.Fatalf("made %d authorization calls; want 1", len(actions))
	}
	if _, ok := actions[0].(*user.SeeSwarmAction); !ok {
		t.Fatalf("named %q; want %q", actions[0].ObjectType(), user.SeeSwarmAction{}.ObjectType())
	}
	if !actions[0].Actor().IsEqual(caller) {
		t.Fatalf("named actor %v; want the caller %v", actions[0].Actor(), caller)
	}
}

// TestNewNodeContractUnclaimedDefaultUserNeedsNoAction: with user omitted on an
// unclaimed node, the op makes no authorization call and answers that user is missing.
func TestNewNodeContractUnclaimedDefaultUserNeedsNoAction(t *testing.T) {
	authority := &recordingAuth{verdict: false}
	mod := newNodeContractModule(t, authority, nil, astral.GenerateIdentity())

	objs := askNewNodeContract(t, mod, astral.GenerateIdentity(), "", "user.new_node_contract")

	msg, ok := objs[0].(*astral.ErrorMessage)
	if !ok {
		t.Fatalf("answered %v; want an error_message", objs[0])
	}
	if got, want := msg.Error(), "user id missing: pass -user <identity>; the node has no active contract to default from"; got != want {
		t.Fatalf("answered %q; want %q", got, want)
	}
	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("made %d authorization calls; want none", n)
	}
}

// TestNewNodeContractDefaultUserAllowed: a caller holding SeeSwarm gets the
// contract issued by the active contract's issuer.
func TestNewNodeContractDefaultUserAllowed(t *testing.T) {
	userID := astral.GenerateIdentity()
	mod := newNodeContractModule(t, &recordingAuth{verdict: true}, userID, astral.GenerateIdentity())
	nodeID := mod.node.Identity()

	objs := askNewNodeContract(t, mod, astral.GenerateIdentity(), "", "user.new_node_contract")

	contract, ok := objs[0].(*auth.Contract)
	if !ok {
		t.Fatalf("answered %v; want a contract", objs[0])
	}
	if !contract.Issuer.IsEqual(userID) || !contract.Subject.IsEqual(nodeID) {
		t.Fatalf("contract %v -> %v; want %v -> %v", contract.Issuer, contract.Subject, userID, nodeID)
	}
}

// TestNewNodeContractNamedUserNeedsNoAction: a caller that names the user gets the
// contract with no authorization call, so the claim ceremony runs unchanged.
func TestNewNodeContractNamedUserNeedsNoAction(t *testing.T) {
	authority := &recordingAuth{verdict: false}
	named := astral.GenerateIdentity()
	mod := newNodeContractModule(t, authority, astral.GenerateIdentity(), named)

	objs := askNewNodeContract(t, mod, astral.GenerateIdentity(), "", "user.new_node_contract?user=anyone")

	contract, ok := objs[0].(*auth.Contract)
	if !ok {
		t.Fatalf("answered %v; want a contract", objs[0])
	}
	if !contract.Issuer.IsEqual(named) {
		t.Fatalf("contract issuer %v; want the named user %v", contract.Issuer, named)
	}
	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("made %d authorization calls; want none", n)
	}
}
