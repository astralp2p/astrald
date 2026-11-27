package user

import (
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/scheduler"
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
// and closes its links with the subject.
func TestSweepRepairsAMemberThatMissedABanOverAStableLink(t *testing.T) {
	userID := astral.GenerateIdentity()
	issuer, member := newReceiverFixture(t, userID), newReceiverFixture(t, userID)
	issuer.auth.seed(nodeContract(userID, member.nodeID))
	issuer.nodes.linked = []*astral.Identity{member.nodeID}
	issuer.mod.Deps.Objects = &forwardPusher{sender: issuer.nodeID, member: member}

	subject := astral.GenerateIdentity()
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
	select {
	case closed := <-member.nodes.closed:
		if !closed.IsEqual(subject) {
			t.Fatalf("the member closed links with %v; want the subject", closed)
		}
	case <-time.After(effectTimeout):
		t.Fatal("the member did not close its links with the subject")
	}
}

// volume sizes the swarm of the push volume test: 10 nodes, 5 bans.
const (
	volumeMembers = 9 // members besides this node
	volumeBans    = 5
)

// TestResyncVolumeTenNodesFiveBans (eZAbta4YIt AC5) counts the work of the
// interim re-sync on one node of a 10-node swarm with 5 bans:
//   - each link event from a member schedules exactly 1 SyncNodesTask, at any
//     LinkCount (2 events per member: 18 runs);
//   - each link event from a banned subject schedules none and pushes the 5
//     bans to it (25 pushes); each subject's answer to its own ban enforces the
//     link policy once (5 passes), after which its links close;
//   - one sweep tick pushes the 5 bans to each of the 9 linked members
//     (45 pushes) and to no subject, since the answered subjects are unlinked;
//   - one SyncNodesTask run pushes 1 active contract, the 5 bans and the 8
//     other members' contracts (14 pushes).
func TestResyncVolumeTenNodesFiveBans(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	f.scheduler.tasks = make(chan scheduler.Task, 64)
	pusher := &banPusher{pushed: make(chan banPush, 256)}
	f.mod.Deps.Objects = pusher

	var members, subjects []*astral.Identity
	for range volumeMembers {
		members = append(members, f.addSibling())
	}
	for range volumeBans {
		subjects = append(subjects, f.ban(t))
	}
	f.mod.refreshBans()

	for _, m := range members {
		for _, count := range []int{1, 2} {
			if err := f.mod.ReceiveObject(localLinkEvent(f, m, count)); err != nil {
				t.Fatalf("ReceiveObject: %v", err)
			}
		}
	}
	if runs := len(f.scheduledSyncs()); runs != 2*volumeMembers {
		t.Fatalf("member link events scheduled %d SyncNodesTask runs; want %d", runs, 2*volumeMembers)
	}

	for _, s := range subjects {
		if err := f.mod.ReceiveObject(localLinkEvent(f, s, 1)); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}
	}
	pushes := collectBanPushes(t, pusher, volumeBans*volumeBans, quiet)
	perTarget, _ := countPushes(pushes)
	for _, s := range subjects {
		if perTarget[s.String()] != volumeBans {
			t.Fatalf("pushed %d bans to subject %v; want %d", perTarget[s.String()], s, volumeBans)
		}
	}
	if runs := len(f.scheduledSyncs()); runs != 0 {
		t.Fatalf("subject link events scheduled %d SyncNodesTask runs; want none", runs)
	}
	if passes := f.nodes.enforced.Load(); passes != volumeBans {
		t.Fatalf("subject answers ran %d enforcement passes; want %d", passes, volumeBans)
	}
	for _, s := range subjects {
		requireRefused(t, f.mod, s)
	}

	f.nodes.linked = members
	runTicks(t, f) <- time.Now()
	pushes = collectBanPushes(t, pusher, volumeMembers*volumeBans, quiet)
	perTarget, _ = countPushes(pushes)
	for _, m := range members {
		if perTarget[m.String()] != volumeBans {
			t.Fatalf("the sweep pushed %d bans to member %v; want %d", perTarget[m.String()], m, volumeBans)
		}
	}

	if got := syncRunPushes(t); !slices.Equal(got, []int{1, volumeBans, volumeMembers - 1}) {
		t.Fatalf("one SyncNodesTask run pushed [active contract, bans, member contracts] = %v; want [1 %d %d]",
			got, volumeBans, volumeMembers-1)
	}
}

// syncRunPushes runs one SyncNodesTask from a node of a 10-node swarm with 5
// bans toward one member and counts its pushes of the active contract, of bans
// and of other members' contracts.
func syncRunPushes(t *testing.T) []int {
	t.Helper()
	ctx, cancel := astral.NewContext(nil).WithTimeout(10 * time.Second)
	defer cancel()

	userID, nodeID := astral.GenerateIdentity(), astral.GenerateIdentity()
	active := nodeContract(userID, nodeID)
	index := &indexAuth{}
	index.seed(active)
	var members []*astral.Identity
	for range volumeMembers {
		m := astral.GenerateIdentity()
		members = append(members, m)
		index.seed(nodeContract(userID, m))
	}

	steps := &syncSteps{}
	kind := func(obj astral.Object) string {
		switch o := obj.(type) {
		case *user.SignedExpulsion:
			return "ban"
		case *auth.SignedContract:
			if o.Subject.IsEqual(nodeID) {
				return "active"
			}
			return "member"
		}
		return obj.ObjectType()
	}
	mod := &Module{
		Deps: Deps{Auth: index, Objects: &stepPusher{steps: steps, kind: kind}},
		ctx:  ctx,
		node: &stepNode{id: nodeID, steps: steps},
		log:  log.New(nodeID),
		db:   testDB(t),
	}
	if err := mod.config.ActiveContract.Set(nil, active); err != nil {
		t.Fatalf("seed active contract: %v", err)
	}
	for range volumeBans {
		if err := mod.db.StoreExpulsion(sampleSigned(userID, astral.GenerateIdentity())); err != nil {
			t.Fatalf("store ban: %v", err)
		}
	}

	if err := mod.NewSyncNodesTask(members[0]).Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	counts := map[string]int{}
	for _, s := range steps.list() {
		counts[s]++
	}
	return []int{counts["push active"], counts["push ban"], counts["push member"]}
}
