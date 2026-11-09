package auth

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
)

func relayPermit() *auth.Permit {
	return &auth.Permit{Action: astral.String8(nodes.RelayForAction{}.ObjectType())}
}

// relayAccepted asks what a node asks when a peer relays a query for forID
// (mod/nodes/src/mux.go handleRelayQuery).
func relayAccepted(mod *Module, relayer, forID *astral.Identity) bool {
	return mod.Authorize(astral.NewContext(nil), &nodes.RelayForAction{
		Action: auth.NewAction(relayer),
		ForID:  forID,
	})
}

// TestWithdrawRelayContractStopsRelayedQueries: an app's and a mailbox's relay
// contract each admit a relayed query until the operator withdraws it; the
// withdrawal refuses the identity it names and no other, and indexing the
// contract again does not bring it back.
func TestWithdrawRelayContractStopsRelayedQueries(t *testing.T) {
	mod := testModule(t)
	// why: the nodes module's own rule, which a contract delegates to: an actor
	// relays for its own identity.
	mod.Add(authmod.Func[*nodes.RelayForAction](func(_ *astral.Context, a *nodes.RelayForAction) bool {
		return a.Actor().IsEqual(a.ForID)
	}))

	host := astral.GenerateIdentity()
	app, mailbox, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()
	appContract := signedContract(app, host, time.Hour, relayPermit())
	for _, who := range []*astral.Identity{app, mailbox, other} {
		sc := appContract
		if who != app {
			sc = signedContract(who, host, time.Hour, relayPermit())
		}
		storeContract(t, mod, sc)
		if !relayAccepted(mod, host, who) {
			t.Fatalf("before the withdrawal the host cannot relay for %v", who)
		}
	}

	for _, who := range []*astral.Identity{app, mailbox} {
		n, err := mod.WithdrawContracts(who, &nodes.RelayForAction{})
		if err != nil || n != 1 {
			t.Fatalf("WithdrawContracts(%v) = %v, %v; want 1, nil", who, n, err)
		}
		if relayAccepted(mod, host, who) {
			t.Errorf("after the withdrawal the host still relays for %v", who)
		}
	}

	if !relayAccepted(mod, host, other) {
		t.Error("withdrawing two identities refused a third")
	}

	// the indexer and a sibling's push index the same object again
	if err := mod.IndexContract(astral.NewContext(nil), appContract); err != nil {
		t.Fatalf("index again: %v", err)
	}
	if relayAccepted(mod, host, app) {
		t.Error("indexing the contract again revived it")
	}

	n, err := mod.WithdrawContracts(app, &nodes.RelayForAction{})
	if err != nil || n != 0 {
		t.Errorf("second WithdrawContracts = %v, %v; want 0, nil", n, err)
	}
}

// TestWithdrawContractsTouchesOnlyTheAction: a contract of the issuer that holds
// no permit for the action stays.
func TestWithdrawContractsTouchesOnlyTheAction(t *testing.T) {
	mod := testModule(t)
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()
	storeContract(t, mod, signedContract(issuer, subject, time.Hour, permit(0)))

	n, err := mod.WithdrawContracts(issuer, &nodes.RelayForAction{})
	if err != nil || n != 0 {
		t.Fatalf("WithdrawContracts = %v, %v; want 0, nil", n, err)
	}

	found, err := mod.SignedContracts().WithIssuer(issuer).Find(astral.NewContext(nil))
	if err != nil || len(found) != 1 {
		t.Fatalf("contracts of the issuer = %v, %v; want the one test.action contract", len(found), err)
	}
}
