package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

func registerSignerGrant() *auth.Permit {
	return &auth.Permit{Action: astral.String8(auth.RegisterSignerAction{}.ObjectType())}
}

// registerSignerAction is the shape crypto.register_signer authorizes each key
// with (mod/crypto/src/op_register_signer.go).
func registerSignerAction(actor *astral.Identity) *auth.RegisterSignerAction {
	return &auth.RegisterSignerAction{Action: auth.NewAction(actor)}
}

// TestRegisterSignerGrantAuthorizesTheGrantedApp is the node-local grant path: an
// app this node granted the action claims keys as itself, and nobody else does.
func TestRegisterSignerGrantAuthorizesTheGrantedApp(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	if err := mod.Grant(app, registerSignerGrant(), nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if !mod.AuthorizeRegisterSigner(nil, registerSignerAction(app)) {
		t.Fatal("the granted app was refused")
	}

	if mod.AuthorizeRegisterSigner(nil, registerSignerAction(astral.GenerateIdentity())) {
		t.Fatal("an identity holding no grant was allowed")
	}
}

// TestRegisterSignerGrantRefusesAConstrainedGrant: nothing evaluates a constraint
// on this action, so a narrowed grant must not read as a grant in full.
func TestRegisterSignerGrantRefusesAConstrainedGrant(t *testing.T) {
	mod := testGrantModule(t)
	app := astral.GenerateIdentity()

	narrowed := registerSignerGrant()
	narrowed.Constraints = astral.NewBundle()
	if err := narrowed.Constraints.Append(&astral.Ack{}); err != nil {
		t.Fatalf("constrain: %v", err)
	}
	if err := mod.Grant(app, narrowed, nil); err != nil {
		t.Fatalf("grant: %v", err)
	}

	if mod.AuthorizeRegisterSigner(nil, registerSignerAction(app)) {
		t.Fatal("a constrained grant was honoured in full")
	}
}
