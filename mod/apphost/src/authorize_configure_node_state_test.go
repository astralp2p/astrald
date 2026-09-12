package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func configureNodeStatePermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.ConfigureNodeStateAction{}.ObjectType())}
}

func configureNodeStateAction(actor *astral.Identity) *auth.ConfigureNodeStateAction {
	return &auth.ConfigureNodeStateAction{Action: auth.NewAction(actor)}
}

// TestConfigureNodeStateGrantAuthorizesGranteeOnly covers the node-local path
// to ConfigureNodeState: an identity this node has granted the action is
// allowed, and an identity it has not is refused.
func TestConfigureNodeStateGrantAuthorizesGranteeOnly(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, configureNodeStatePermit(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if !mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(app)) {
		t.Fatal("a granted identity was refused")
	}

	if mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(astral.GenerateIdentity())) {
		t.Fatal("an ungranted identity was allowed")
	}
}

// TestConfigureNodeStateGrantRefusesConstrainedPermit covers the action's
// refusal of constraints: a grant narrowed by any constraint authorizes
// nothing, because the action evaluates no constraint.
func TestConfigureNodeStateGrantRefusesConstrainedPermit(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	permit := configureNodeStatePermit()
	permit.Constraints = astral.NewBundle()
	if err := permit.Constraints.Append(astral.NewString8("/mod/tcp")); err != nil {
		t.Fatalf("constrain: %v", err)
	}

	if err := mod.Grant(app, permit, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if mod.AuthorizeConfigureNodeState(nil, configureNodeStateAction(app)) {
		t.Fatal("a constrained grant was honoured in full")
	}
}
