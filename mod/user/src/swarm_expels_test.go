package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// TestSwarmStateExpelIsStoredAndApplied: an Expulsion by the user that reaches
// this node only through SwarmState is stored as a ban, as a pushed one is:
// link admission refuses the subject once it answers, the subject, no member
// here, is published as left (mod/nodes then closes its links), and a second
// change applies nothing again. An Expulsion by another issuer is not stored.
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
	events := f.watchSwarm(t)

	f.mod.receiveSwarmExpels()

	if f.mod.banOn(subject) == nil {
		t.Fatal("the user's expel in SwarmState was not stored as a ban")
	}
	if f.mod.banOn(stranger) != nil {
		t.Fatal("an expel by another issuer was stored as a ban")
	}
	requireBanned(t, events, subject)

	f.mod.receiveSwarmExpels()
	time.Sleep(quiet)
	if got := events.take(); len(got) != 0 {
		t.Fatalf("a second change published %v; want nothing", got)
	}
}

// TestPublishBanEmitsOncePerNode: a ban that takes effect again, as when it
// is pushed after it was read from SwarmState, emits no second
// SwarmNodeBanned.
func TestPublishBanEmitsOncePerNode(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	subject := astral.GenerateIdentity()
	events := f.watchSwarm(t)

	f.mod.publishBan(subject)
	f.mod.publishBan(subject)
	got := events.take()
	if len(got) != 1 {
		t.Fatalf("published %v; want one SwarmNodeBanned", got)
	}
	if e, ok := got[0].(*user.SwarmNodeBanned); !ok || !e.NodeID.IsEqual(subject) {
		t.Fatalf("published %v; want the ban of %v", got[0], subject)
	}
}
