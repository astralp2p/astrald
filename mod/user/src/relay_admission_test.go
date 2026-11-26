package user

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// TestAdmitRelayContract covers the one rule for relay-for contracts on every
// path: a delivering sender (push, forward, pull) and a local path (nil sender:
// app registration, the auth indexer).
func TestAdmitRelayContract(t *testing.T) {
	cases := []struct {
		name   string
		noUser bool
		// build returns the sender and the contract; admit reports whether the rule admits it.
		build func(f *receiverFixture) (*astral.Identity, *auth.SignedContract)
		admit bool
	}{
		{"member subject forwarded by another member", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return f.addSibling(), relayForContract(astral.GenerateIdentity(), f.addSibling())
		}, true},
		{"member subject delivered by the subject", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			host := f.addSibling()
			return host, relayForContract(astral.GenerateIdentity(), host)
		}, true},
		{"member subject on a local path", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), f.addSibling())
		}, true},
		{"this node as subject on a claimed node", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), f.nodeID)
		}, true},
		{"this node as subject on an unclaimed node", true, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), f.nodeID)
		}, true},
		{"a contract of another kind passes through", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return astral.GenerateIdentity(), membershipContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}, true},
		{"expelled subject on a local path", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), f.expelSibling())
		}, false},
		{"expelled subject forwarded by a member", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return f.addSibling(), relayForContract(astral.GenerateIdentity(), f.expelSibling())
		}, false},
		{"member subject delivered by a node outside the swarm", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return astral.GenerateIdentity(), relayForContract(astral.GenerateIdentity(), f.addSibling())
		}, false},
		{"subject outside the swarm", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}, false},
		{"another node as subject on an unclaimed node", true, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity())
		}, false},
		{"forged issuer signature", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			sc := relayForContract(astral.GenerateIdentity(), f.addSibling())
			sc.IssuerSig = forgedSig()
			return f.addSibling(), sc
		}, false},
		{"missing subject signature", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			sc := relayForContract(astral.GenerateIdentity(), f.addSibling())
			sc.SubjectSig = nil
			return nil, sc
		}, false},
		{"expired contract", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			sc := relayForContract(astral.GenerateIdentity(), f.addSibling())
			sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))
			return nil, sc
		}, false},
		{"relay-for beside another permit, subject outside the swarm", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, contractWith(astral.GenerateIdentity(), astral.GenerateIdentity(), nodes.RelayForAction{}.ObjectType(), "mod.objects.read_action")
		}, false},
		{"relay-for beside another permit, member subject", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, contractWith(astral.GenerateIdentity(), f.addSibling(), nodes.RelayForAction{}.ObjectType(), "mod.objects.read_action")
		}, true},
		{"contract without an issuer", false, func(f *receiverFixture) (*astral.Identity, *auth.SignedContract) {
			return nil, relayForContract(nil, f.addSibling())
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var userID *astral.Identity
			if !tc.noUser {
				userID = astral.GenerateIdentity()
			}
			f := newReceiverFixture(t, userID)
			sender, sc := tc.build(f)

			err := f.mod.AdmitRelayContract(sender, sc)
			if tc.admit && err != nil {
				t.Fatalf("AdmitRelayContract: %v; want admitted", err)
			}
			if !tc.admit && err == nil {
				t.Fatal("AdmitRelayContract admitted the contract; want a refusal")
			}
		})
	}
}

// TestAdmitRelayContractExpiredIsErrContractExpired: an expired contract is
// refused with auth.ErrContractExpired, as the push path refused it before.
func TestAdmitRelayContractExpiredIsErrContractExpired(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	sc := relayForContract(astral.GenerateIdentity(), f.addSibling())
	sc.ExpiresAt = astral.Time(time.Now().Add(-time.Minute))

	if err := f.mod.AdmitRelayContract(nil, sc); !errors.Is(err, auth.ErrContractExpired) {
		t.Fatalf("AdmitRelayContract: %v; want auth.ErrContractExpired", err)
	}
}

// expelSibling makes a node a member, then expels it, and returns the node.
func (f *receiverFixture) expelSibling() *astral.Identity {
	node := f.addSibling()
	if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, node)); err != nil {
		panic(err)
	}
	return node
}
