package user

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

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

// fakeDigest is a sibling's digest: what it answers and how each ask reached it.
type fakeDigest struct {
	mu       sync.Mutex
	digest   relayDigest
	err      error
	block    bool
	siblings []*astral.Identity
	deadline []time.Time
}

func (d *fakeDigest) ask(ctx *astral.Context, sibling *astral.Identity) (relayDigest, error) {
	d.mu.Lock()
	d.siblings = append(d.siblings, sibling)
	deadline, _ := ctx.Deadline()
	d.deadline = append(d.deadline, deadline)
	block := d.block
	d.mu.Unlock()

	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return d.digest, d.err
}

func digestOf(t *testing.T, held ...*auth.SignedContract) relayDigest {
	t.Helper()

	digest := relayDigest{}
	for _, sc := range held {
		digest[objectIDOf(t, sc)] = struct{}{}
	}
	return digest
}

// digestFixture is a node with the contracts of n apps its member host, and a
// sibling that has not linked yet.
func digestFixture(t *testing.T, n int) (f *receiverFixture, pushes *pushRecorder, sibling *astral.Identity, contracts []*auth.SignedContract) {
	t.Helper()

	f = newReceiverFixture(t, astral.GenerateIdentity())
	pushes = &pushRecorder{}
	f.mod.Deps.Objects = pushes

	sibling, host := f.addSibling(), f.addSibling()
	for range n {
		sc := relayForContract(astral.GenerateIdentity(), host)
		f.auth.seed(sc)
		contracts = append(contracts, sc)
	}
	return f, pushes, sibling, contracts
}

// TestSyncAppContractsPushesOnlyTheMissingContract: with 40 forwardable
// contracts and a sibling whose digest lists 39, exactly the 1 missing contract
// is pushed, to the sibling, within a bounded push.
func TestSyncAppContractsPushesOnlyTheMissingContract(t *testing.T) {
	f, pushes, sibling, contracts := digestFixture(t, 40)
	digest := &fakeDigest{digest: digestOf(t, contracts[:39]...)}
	f.mod.askDigest = digest.ask

	f.mod.syncAppContracts(astral.NewContext(nil), sibling)

	if len(pushes.pushed) != 1 || pushes.pushed[0] != astral.Object(contracts[39]) {
		t.Fatalf("pushed %d objects; want exactly the missing contract", len(pushes.pushed))
	}
	if !pushes.target[0].IsEqual(sibling) || !pushes.deadline[0] {
		t.Fatalf("pushed to %v (bounded %v); want the sibling %v with a deadline", pushes.target[0], pushes.deadline[0], sibling)
	}
}

// TestSyncAppContractsPushesNothingWhenTheSiblingHoldsAll: a digest that lists
// every forwardable contract leaves nothing to push.
func TestSyncAppContractsPushesNothingWhenTheSiblingHoldsAll(t *testing.T) {
	f, pushes, sibling, contracts := digestFixture(t, 40)
	// note: the sibling holds more than it is pushed, such as the contracts of its own apps.
	held := digestOf(t, append(contracts, relayForContract(astral.GenerateIdentity(), sibling))...)
	f.mod.askDigest = (&fakeDigest{digest: held}).ask

	f.mod.syncAppContracts(astral.NewContext(nil), sibling)

	if len(pushes.pushed) != 0 {
		t.Fatalf("pushed %d objects; want none", len(pushes.pushed))
	}
}

// TestSyncAppContractsPushesEveryContractWithoutADigest: a digest that fails
// by error, refusal or timeout, or that lists nothing, never means push
// nothing; every forwardable contract is pushed.
func TestSyncAppContractsPushesEveryContractWithoutADigest(t *testing.T) {
	cases := map[string]*fakeDigest{
		"error":         {err: errors.New("link closed")},
		"refusal":       {err: auth.ErrNotPermitted},
		"timeout":       {block: true},
		"unfinished":    {err: errDigestUnfinished},
		"empty digest":  {digest: relayDigest{}},
		"another peer":  {digest: digestOf(t, relayForContract(astral.GenerateIdentity(), astral.GenerateIdentity()))},
		"nil, no error": {},
	}

	for name, digest := range cases {
		t.Run(name, func(t *testing.T) {
			f, pushes, sibling, contracts := digestFixture(t, 5)
			f.mod.askDigest = digest.ask
			f.mod.digestTimeout = 20 * time.Millisecond

			f.mod.syncAppContracts(astral.NewContext(nil), sibling)

			if len(pushes.pushed) != len(contracts) {
				t.Fatalf("pushed %d objects; want all %d forwardable contracts", len(pushes.pushed), len(contracts))
			}
			for _, sc := range contracts {
				if !slices.Contains(pushes.pushed, astral.Object(sc)) {
					t.Fatalf("contract of %v not pushed", sc.Issuer)
				}
			}
		})
	}
}

// TestSyncAppContractsAsksOneDigestWithItsOwnDeadline: one digest query goes to
// the sibling, bounded by relayDigestTimeout (10 s, the spec's bound) apart
// from the push bound.
func TestSyncAppContractsAsksOneDigestWithItsOwnDeadline(t *testing.T) {
	f, _, sibling, _ := digestFixture(t, 3)
	digest := &fakeDigest{}
	f.mod.askDigest = digest.ask

	before := time.Now()
	f.mod.syncAppContracts(astral.NewContext(nil), sibling)

	if len(digest.siblings) != 1 || !digest.siblings[0].IsEqual(sibling) {
		t.Fatalf("digest asked of %v; want one ask of the sibling %v", digest.siblings, sibling)
	}
	left := digest.deadline[0].Sub(before)
	if digest.deadline[0].IsZero() || left > relayDigestTimeout+time.Second || left < relayDigestTimeout-time.Second {
		t.Fatalf("digest deadline in %v; want about %v", left, relayDigestTimeout)
	}
	if relayDigestTimeout != 10*time.Second {
		t.Fatalf("relayDigestTimeout is %v; the spec states 10 s", relayDigestTimeout)
	}
}

// TestSyncAppContractsAsksNoDigestWithNothingToPush: no forwardable contract
// leaves no digest to ask.
func TestSyncAppContractsAsksNoDigestWithNothingToPush(t *testing.T) {
	f, pushes, sibling, _ := digestFixture(t, 0)
	digest := &fakeDigest{}
	f.mod.askDigest = digest.ask
	f.auth.seed(relayForContract(astral.GenerateIdentity(), sibling)) // the sibling's own app

	f.mod.syncAppContracts(astral.NewContext(nil), sibling)

	if len(digest.siblings) != 0 || len(pushes.pushed) != 0 {
		t.Fatalf("asked %d digests and pushed %d objects; want none", len(digest.siblings), len(pushes.pushed))
	}
}

// TestSyncAppContractsPushesAContractTheReceiverRefusedEarlier: the receiver's
// own OpRelayDigest, read through the real client, omits a contract it refused,
// so every sync pushes it again until the receiver holds it, and then pushes
// nothing.
func TestSyncAppContractsPushesAContractTheReceiverRefusedEarlier(t *testing.T) {
	userID := astral.GenerateIdentity()
	receiver := newReceiverFixture(t, userID)

	// note: both nodes belong to the user's swarm; the pusher's sibling is the receiver.
	pusher := newReceiverFixture(t, userID)
	pushes := &pushRecorder{}
	pusher.mod.Deps.Objects = pushes
	pusher.mod.askDigest = nil // the real client
	pusher.mod.node = &opNode{id: pusher.nodeID, op: handlerOf(t, receiver)}
	receiver.auth.seed(membershipContract(userID, pusher.nodeID))
	pusher.auth.seed(membershipContract(userID, receiver.nodeID))

	host := pusher.addSibling()
	held, refused := relayForContract(astral.GenerateIdentity(), host), relayForContract(astral.GenerateIdentity(), host)
	for _, sc := range []*auth.SignedContract{held, refused} {
		pusher.auth.seed(sc)
	}
	receiver.auth.seed(held) // the receiver admitted this one, and refused the other

	for run := range 2 {
		pushes.pushed = nil
		pusher.mod.syncAppContracts(astral.NewContext(nil), receiver.nodeID)
		if len(pushes.pushed) != 1 || pushes.pushed[0] != astral.Object(refused) {
			t.Fatalf("sync %d pushed %d objects; want the refused contract alone", run+1, len(pushes.pushed))
		}
	}

	receiver.auth.seed(refused)
	pushes.pushed = nil
	pusher.mod.syncAppContracts(astral.NewContext(nil), receiver.nodeID)
	if len(pushes.pushed) != 0 {
		t.Fatalf("sync after the receiver held every contract pushed %d objects; want none", len(pushes.pushed))
	}
}
