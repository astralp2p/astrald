package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// expelNodes is a nodes module whose SwarmState holds the given expels.
type expelNodes struct {
	*recordingNodes
	expels []*user.SignedExpulsion
}

func (n *expelNodes) SwarmEntries(kind user.EntryKind) ([]*astral.ObjectID, error) {
	if kind != user.EntryExpels {
		return nil, nil
	}
	var ids []*astral.ObjectID
	for _, se := range n.expels {
		id, err := astral.ResolveObjectID(se)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (n *expelNodes) SwarmEntry(id *astral.ObjectID) (astral.Object, error) {
	for _, se := range n.expels {
		if got, _ := astral.ResolveObjectID(se); got.IsEqual(id) {
			return se, nil
		}
	}
	return nil, astral.NewError("no such entry")
}

// TestSwarmStateExpelIsStoredAndApplied: an Expulsion by the user that reaches
// this node only through SwarmState is stored as a ban, as a pushed one is:
// link admission refuses the subject once it answers, this node closes its
// links with it, and a second change stores nothing again. An Expulsion by
// another issuer is not stored.
func TestSwarmStateExpelIsStoredAndApplied(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	subject, stranger := astral.GenerateIdentity(), astral.GenerateIdentity()
	nodes := &expelNodes{recordingNodes: f.nodes, expels: []*user.SignedExpulsion{
		sampleSignedValid(userID, subject),
		sampleSignedValid(astral.GenerateIdentity(), stranger),
	}}
	f.mod.Deps.Nodes = nodes

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
