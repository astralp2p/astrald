package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// forwardPusher delivers every push to member's node to member's receiver, as
// a push over a stable link does; it drops pushes to other nodes.
type forwardPusher struct {
	objectsmod.Module
	sender *astral.Identity
	member *receiverFixture
}

func (p *forwardPusher) Push(_ *astral.Context, target *astral.Identity, obj astral.Object) error {
	if !target.IsEqual(p.member.nodeID) {
		return nil
	}
	return p.member.mod.ReceiveObject(&recordingDrop{sender: p.sender, object: obj})
}

// runTicks runs the sweep of f on tick until the test ends.
func runTicks(t *testing.T, f *receiverFixture) chan<- time.Time {
	t.Helper()
	tick := make(chan time.Time)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.mod.reconcileOnTick(ctx, tick)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return tick
}

// holdsBan reports whether f stored the user's ban on subject.
func holdsBan(f *receiverFixture, subject *astral.Identity) bool {
	_, err := f.mod.db.ExpulsionFor(f.userID, subject)
	return err == nil
}

// TestSweepRepairsAMemberThatMissedABanOverAStableLink (eZAbta4YIt AC4): a
// member that missed a ban keeps one link to the issuer up, so no
// LinkCreatedEvent fires; the member does not hold the ban until the issuer's
// sweep ticks once (the tick stands in for the 1 h ticker), and then holds it
// and publishes that the subject left, on which mod/nodes closes its links.
func TestSweepRepairsAMemberThatMissedABanOverAStableLink(t *testing.T) {
	userID := astral.GenerateIdentity()
	issuer, member := newReceiverFixture(t, userID), newReceiverFixture(t, userID)
	issuer.auth.seed(nodeContract(userID, member.nodeID))
	issuer.nodes.linked = []*astral.Identity{member.nodeID}
	issuer.mod.Deps.Objects = &forwardPusher{sender: issuer.nodeID, member: member}

	subject := astral.GenerateIdentity()
	events := member.watchMember(t, subject)
	if err := issuer.mod.db.StoreExpulsion(sampleSignedValid(userID, subject)); err != nil {
		t.Fatalf("store ban: %v", err)
	}

	tick := runTicks(t, issuer)
	time.Sleep(quiet)
	if holdsBan(member, subject) {
		t.Fatal("the member holds the ban before the sweep")
	}

	tick <- time.Now()
	deadline := time.Now().Add(effectTimeout)
	for !holdsBan(member, subject) {
		if time.Now().After(deadline) {
			t.Fatal("the member does not hold the ban one sweep after it was missed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	requireLeft(t, events, subject)
}

// volume sizes the swarm of the push volume test: 10 nodes, 5 bans.
