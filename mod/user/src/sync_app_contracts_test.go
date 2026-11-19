package user

import (
	"slices"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

type pushRecorder struct {
	objects.Module
	mu       sync.Mutex
	target   []*astral.Identity
	pushed   []astral.Object
	deadline []bool
}

func (p *pushRecorder) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, bounded := ctx.Deadline()
	p.target = append(p.target, target)
	p.pushed = append(p.pushed, obj)
	p.deadline = append(p.deadline, bounded)
	return nil
}

// TestSyncAppContractsForwardsEveryMembersRelayContracts: the sibling receives
// the relay contracts of the apps this node and every other member host, and
// none it holds as host, none of an expelled or unknown host, and none that
// carries another permit. Each push is bounded.
func TestSyncAppContractsForwardsEveryMembersRelayContracts(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	pushes := &pushRecorder{}
	f.mod.Deps.Objects = pushes

	receiver, member := f.addSibling(), f.addSibling()
	own := relayForContract(astral.GenerateIdentity(), f.nodeID)
	members := relayForContract(astral.GenerateIdentity(), member)
	for _, sc := range []*auth.SignedContract{
		own,
		members,
		relayForContract(astral.GenerateIdentity(), receiver),
		relayForContract(astral.GenerateIdentity(), f.expelSibling()),
		relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity()),
		contractWith(astral.GenerateIdentity(), member, nodes.RelayForAction{}.ObjectType(), auth.SeeObjectsAction{}.ObjectType()),
	} {
		f.auth.seed(sc)
	}

	f.mod.syncAppContracts(astral.NewContext(nil), receiver)

	if len(pushes.pushed) != 2 || !slices.Contains(pushes.pushed, astral.Object(own)) || !slices.Contains(pushes.pushed, astral.Object(members)) {
		t.Fatalf("pushed %d objects; want this node's and the other member's relay contracts", len(pushes.pushed))
	}
	for i, target := range pushes.target {
		if !target.IsEqual(receiver) {
			t.Fatalf("pushed to %v; want the sibling %v", target, receiver)
		}
		if !pushes.deadline[i] {
			t.Fatal("a push ran without a deadline")
		}
	}
}
