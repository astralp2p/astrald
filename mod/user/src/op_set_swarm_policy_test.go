package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/policy"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

// swarmPolicySetter is a module whose setters authorize every caller when
// admin is set and resolve every name to delegateID. Its delegate values are
// unbound, so a write lands in memory, as a bound value's does on a
// successful save.
func swarmPolicySetter(admin bool, delegateID *astral.Identity) *Module {
	return &Module{
		Deps: Deps{Auth: &recordingAuth{verdict: admin}, Dir: &resolvingDir{id: delegateID}},
		node: &identityNode{id: astral.GenerateIdentity()},
		log:  log.New(nil),
	}
}

func setSwarmPolicyOp(t *testing.T, fn any, query string) (*recordingWriter, error) {
	t.Helper()

	w := newRecordingWriter()
	err := route(t, fn, astral.GenerateIdentity(), query, w)
	return w, err
}

func awaitSetterClose(t *testing.T, w *recordingWriter) {
	t.Helper()

	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		t.Fatal("the setter never closed the caller's connection")
	}
}

// A caller without AdminManageApps is rejected before anything is written,
// and AdminManageApps is the one action asked about.
func TestSetSwarmPolicyRequiresAdminManageApps(t *testing.T) {
	mod := swarmPolicySetter(false, astral.GenerateIdentity())

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmJoinPolicy, "user.set_swarm_join_policy?identity=cp&operation=control.decide_swarm")
	if err == nil {
		t.Fatal("a caller without AdminManageApps set the policy")
	}
	if w.written() != 0 {
		t.Fatal("a rejected setter answered")
	}
	if !mod.policy.SwarmJoin.Get().IsZero() {
		t.Fatal("a rejected setter wrote a delegate")
	}
	asked := mod.Auth.(*recordingAuth).recorded()
	if len(asked) != 1 || asked[0].ObjectType() != (auth.AdminManageAppsAction{}).ObjectType() {
		t.Fatalf("the setter asked about %v, want AdminManageApps alone", asked)
	}
}

// Identity, operation, and timeout are persisted together for the join policy
// alone, and the getter then selects the delegated policy.
func TestSetSwarmJoinPolicyPersistsTheDelegate(t *testing.T) {
	id := astral.GenerateIdentity()
	mod := swarmPolicySetter(true, id)

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmJoinPolicy, "user.set_swarm_join_policy?identity=cp&operation=control.decide_swarm&timeout=2m")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitSetterClose(t, w)

	d := mod.policy.SwarmJoin.Get()
	if d.IsZero() || !d.Identity.IsEqual(id) || d.Operation != "control.decide_swarm" || time.Duration(d.Timeout) != 2*time.Minute {
		t.Fatalf("persisted %+v, want the delegate as set", d)
	}
	if !mod.policy.SwarmInvite.Get().IsZero() {
		t.Fatal("the join setter wrote the invite delegate")
	}
}

// The invite setter writes the invite delegate alone.
func TestSetSwarmInvitePolicyPersistsTheDelegate(t *testing.T) {
	id := astral.GenerateIdentity()
	mod := swarmPolicySetter(true, id)

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmInvitePolicy, "user.set_swarm_invite_policy?identity=cp&operation=control.decide_invite")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitSetterClose(t, w)

	d := mod.policy.SwarmInvite.Get()
	if d.IsZero() || !d.Identity.IsEqual(id) || d.Operation != "control.decide_invite" {
		t.Fatalf("persisted %+v, want the delegate as set", d)
	}
	if !mod.policy.SwarmJoin.Get().IsZero() {
		t.Fatal("the invite setter wrote the join delegate")
	}
}

// The contract accept setter writes the contract accept delegate alone.
func TestSetContractAcceptPolicyPersistsTheDelegate(t *testing.T) {
	id := astral.GenerateIdentity()
	mod := swarmPolicySetter(true, id)

	w, err := setSwarmPolicyOp(t, mod.OpSetContractAcceptPolicy, "user.set_contract_accept_policy?identity=cp&operation=control.decide_contract")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitSetterClose(t, w)

	d := mod.policy.ContractAccept.Get()
	if d.IsZero() || !d.Identity.IsEqual(id) || d.Operation != "control.decide_contract" {
		t.Fatalf("persisted %+v, want the delegate as set", d)
	}
	if !mod.policy.SwarmJoin.Get().IsZero() || !mod.policy.SwarmInvite.Get().IsZero() {
		t.Fatal("the contract accept setter wrote a swarm delegate")
	}
}

// A timeout above the node's bound is refused before persistence.
func TestSetSwarmPolicyRefusesATimeoutAboveTheBound(t *testing.T) {
	mod := swarmPolicySetter(true, astral.GenerateIdentity())

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmJoinPolicy, "user.set_swarm_join_policy?identity=cp&operation=control.decide_swarm&timeout=11m")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitSetterClose(t, w)

	if !mod.policy.SwarmJoin.Get().IsZero() {
		t.Fatalf("a timeout above %v was persisted", policy.MaxTimeout)
	}
}

// The node cannot be its own delegate.
func TestSetSwarmPolicyRefusesTheNodeItself(t *testing.T) {
	mod := swarmPolicySetter(true, nil)
	mod.Dir = &resolvingDir{id: mod.node.Identity()}

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmJoinPolicy, "user.set_swarm_join_policy?identity=self&operation=user.request_membership")
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	awaitSetterClose(t, w)

	if !mod.policy.SwarmJoin.Get().IsZero() {
		t.Fatal("the node was persisted as its own delegate")
	}
}

// Both empty clears the delegate, which restores accept-all.
func TestSetSwarmPolicyClearsTheDelegate(t *testing.T) {
	mod := swarmPolicySetter(true, astral.GenerateIdentity())
	if err := mod.policy.SwarmJoin.Set(astral.NewContext(nil), delegate()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	w, err := setSwarmPolicyOp(t, mod.OpSetSwarmJoinPolicy, "user.set_swarm_join_policy")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	awaitSetterClose(t, w)

	if !mod.policy.SwarmJoin.Get().IsZero() {
		t.Fatal("an empty set did not clear the delegate")
	}
	if !joinAllowed(t, mod) {
		t.Fatal("clearing the delegate did not restore accept-all")
	}
}
