package user

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	usermod "github.com/astralp2p/astrald/mod/user"
)

// TestSwarmStateExpelIsApplied: an Expulsion by the user that reaches this
// node only through SwarmState bans its subject (IsBanned, which link
// admission and the ban veto read), and the member it names is published as
// left (mod/nodes then closes its links), once. An Expulsion by another issuer
// is not stored.
func TestSwarmStateExpelIsApplied(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	subject, stranger := f.addSibling(), astral.GenerateIdentity()
	events := f.watchSwarm(t)

	banNode(t, f.mod, userID, subject)
	err := f.mod.swarmState.merge(f.mod.swarmState.ctx, sampleSignedValid(astral.GenerateIdentity(), stranger))
	if !errors.Is(err, usermod.ErrSwarmEntryDropped) {
		t.Fatalf("an expel by another issuer: %v; want it dropped", err)
	}

	f.mod.onSwarmChanged()
	if !f.mod.IsBanned(subject) {
		t.Fatal("the user's expel in SwarmState did not ban its subject")
	}
	if f.mod.IsBanned(stranger) {
		t.Fatal("an expel by another issuer banned its subject")
	}
	requireLeft(t, events, subject)

	f.mod.onSwarmChanged()
	requireLeft(t, events)
}

// TestSwarmStateExpelOfThisNodeLeavesTheSwarm: an Expulsion of this node that
// reaches it only through SwarmState clears its active contract, as a pushed
// one does.
func TestSwarmStateExpelOfThisNodeLeavesTheSwarm(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)

	f.mod.onSwarmChanged()
	if f.mod.ActiveContract() == nil {
		t.Fatal("left the swarm with no expel of this node")
	}

	banNode(t, f.mod, userID, f.nodeID)
	f.mod.onSwarmChanged()
	if f.mod.ActiveContract() != nil {
		t.Fatal("still in the swarm after its expel arrived in SwarmState")
	}
}
