package messaging

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// pushRecorder is a user module that records every object pushed to the linked siblings.
// note: the embedded nil interface panics on any other method, which asserts
// that create_mailbox calls nothing else on the user module.
type pushRecorder struct {
	usermod.Module
	pushed []astral.Object
}

func (r *pushRecorder) PushToSiblings(_ *astral.Context, obj astral.Object) {
	r.pushed = append(r.pushed, obj)
}

// TestCreateMailboxPushesTheRelayContractToTheSiblings: create_mailbox pushes
// the new participant's relay-for contract, issued by the participant to this
// node, to the linked siblings, so a sibling linked before the create can
// route a delivery to the mailbox.
func TestCreateMailboxPushesTheRelayContractToTheSiblings(t *testing.T) {
	mod, _, _ := testIdentityModule(t)
	recorder := &pushRecorder{}
	mod.User = recorder

	cred, err := mod.CreateMailbox(mod.ctx, "", 0)
	if err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	if len(recorder.pushed) != 1 {
		t.Fatalf("create_mailbox pushed %d objects to the siblings; want the relay contract alone", len(recorder.pushed))
	}
	signed, ok := recorder.pushed[0].(*auth.SignedContract)
	if !ok {
		t.Fatalf("create_mailbox pushed %v; want a signed contract", recorder.pushed[0].ObjectType())
	}
	if !signed.Issuer.IsEqual(cred.Identity) {
		t.Fatalf("the pushed contract is issued by %v; want the participant %v", signed.Issuer, cred.Identity)
	}
	if !signed.Subject.IsEqual(mod.node.Identity()) {
		t.Fatalf("the pushed contract names subject %v; want this node %v", signed.Subject, mod.node.Identity())
	}
	if len(signed.Permits) != 1 || string(signed.Permits[0].Action) != (nodes.RelayForAction{}).ObjectType() {
		t.Fatalf("the pushed contract carries %d permits; want one %v", len(signed.Permits), (nodes.RelayForAction{}).ObjectType())
	}
	if signed.IssuerSig == nil || signed.SubjectSig == nil {
		t.Fatal("the pushed contract lacks a signature; a sibling indexes a contract both parties signed")
	}
}

// TestAFailedCreatePushesNothing: a create that fails after the relay contract
// is indexed pushes no contract of a mailbox this node does not serve.
func TestAFailedCreatePushesNothing(t *testing.T) {
	mod, apphost, _ := testIdentityModule(t)
	recorder := &pushRecorder{}
	mod.User = recorder
	apphost.createErr = errors.New("token store is down")

	if _, err := mod.CreateMailbox(mod.ctx, "", 0); err == nil {
		t.Fatal("the create answered no error when the token could not be issued")
	}
	if len(recorder.pushed) != 0 {
		t.Fatalf("a failed create pushed %d objects to the siblings; want none", len(recorder.pushed))
	}
}
