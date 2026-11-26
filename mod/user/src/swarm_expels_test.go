package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestSwarmStateExpelIsStoredAndApplied: an Expulsion by the user that reaches
// this node only through SwarmState is stored as a ban, as a pushed one is:
// link admission refuses the subject once it answers, this node closes its
// links with it, and a second change stores nothing again. An Expulsion by
// another issuer is not stored.
func TestSwarmStateExpelIsStoredAndApplied(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	subject, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()
	n := newStateNode(t, newSwarmScript())
	for _, se := range []*user.SignedExpulsion{
		sampleSignedValid(userID, subject),
		sampleSignedValid(astral.GenerateIdentity(), stranger),
	} {
		if err := n.store.merge(n.store.ctx, se); err != nil {
			t.Fatalf("store expel: %v", err)
		}
	}
	f.mod.swarm = n.store

	f.mod.receiveSwarmExpels()

	if f.mod.banOn(subject) == nil {
		t.Fatal("the user's expel in SwarmState was not stored as a ban")
	}
	if f.mod.banOn(stranger) != nil {
		t.Fatal("an expel by another issuer was stored as a ban")
	}
	select {
	case id := <-f.nodes.closed:
		if !id.IsEqual(subject) {
			t.Fatalf("closed links with %v; want the subject %v", id, subject)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the links with the expelled subject were not closed")
	}

	f.mod.receiveSwarmExpels()
	select {
	case id := <-f.nodes.closed:
		t.Fatalf("a second change applied the expel again (closed %v)", id)
	case <-time.After(quiet):
	}
}
