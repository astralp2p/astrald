package user

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
)

// readOnlyNode is a tree node that holds obj and refuses every write.
type readOnlyNode struct {
	tree.Node
	obj astral.Object
}

func (n *readOnlyNode) Get(*astral.Context, bool) (<-chan astral.Object, error) {
	ch := make(chan astral.Object, 1)
	ch <- n.obj
	close(ch)
	return ch, nil
}

func (n *readOnlyNode) Set(*astral.Context, astral.Object) error {
	return errors.New("read-only node")
}

// TestExpelledNodeLeavesItsSwarmWhenTheClearFails: a node that receives its
// own ban publishes that every member left, itself included, and reads an
// empty swarm, though clearing its active contract fails; mod/nodes then
// stops every member's link task and closes the links.
func TestExpelledNodeLeavesItsSwarmWhenTheClearFails(t *testing.T) {
	userID := astral.GenerateIdentity()
	f := newReceiverFixture(t, userID)
	a, b := f.addSibling(), f.addSibling()
	f.auth.seed(nodeContract(userID, f.nodeID))

	f.mod.config.ActiveContract.NoLocal = true
	node := &readOnlyNode{obj: &auth.SignedContract{Contract: &auth.Contract{Issuer: userID}}}
	if err := f.mod.config.ActiveContract.Bind(astral.NewContext(nil), node); err != nil {
		t.Fatalf("bind active contract: %v", err)
	}
	events := f.watchSwarm(t)

	drop := &recordingDrop{sender: astral.GenerateIdentity(), object: sampleSignedValid(userID, f.nodeID)}
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject(own ban): %v", err)
	}

	requireLeft(t, events, a, b, f.nodeID)
	if f.mod.ActiveContract() == nil {
		t.Fatal("the active contract was cleared; want the clear to fail")
	}
	if got := f.mod.LocalSwarm(); len(got) != 0 {
		t.Fatalf("LocalSwarm = %v after this node's own ban; want empty", got)
	}
}
