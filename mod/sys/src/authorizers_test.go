package sys

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func configureNodeState(actor *astral.Identity) *auth.ConfigureNodeStateAction {
	return &auth.ConfigureNodeStateAction{Action: auth.NewAction(actor)}
}

// TestManagerHoldsConfigureNodeState: the manager holds the action in every
// state, a claimed node included; nobody else holds it through sys.
func TestManagerHoldsConfigureNodeState(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod, _, u := freshModule()
	manager := astral.GenerateIdentity()

	if mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("a node without a manager granted the action")
	}

	mod.appointment.Manager.Set(ctx, manager)
	if !mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("the manager does not hold the action")
	}

	u.id = astral.GenerateIdentity() // claimed
	if !mod.AuthorizeConfigureNodeState(ctx, configureNodeState(manager)) {
		t.Fatal("the manager lost the action after the claim")
	}
	if mod.AuthorizeConfigureNodeState(ctx, configureNodeState(astral.GenerateIdentity())) {
		t.Fatal("another identity holds the action")
	}
}
