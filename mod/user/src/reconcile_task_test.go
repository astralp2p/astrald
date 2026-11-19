package user

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// banPush is one push of a ban the user module made.
type banPush struct {
	target   *astral.Identity
	subject  *astral.Identity
	deadline time.Time
}

// banPusher is an objects module that records every pushed ban.
type banPusher struct {
	objectsmod.Module
	pushed chan banPush
}

func (p *banPusher) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	if ban, ok := obj.(*user.SignedExpulsion); ok {
		deadline, _ := ctx.Deadline()
		p.pushed <- banPush{target: target, subject: ban.Subject, deadline: deadline}
	}
	return nil
}

// withBanPusher makes the fixture record its pushed bans.
func (f *receiverFixture) withBanPusher() *banPusher {
	p := &banPusher{pushed: make(chan banPush, 32)}
	f.mod.Deps.Objects = p
	return p
}

// ban stores a ban of the current user on a new node and returns the node.
func (f *receiverFixture) ban(t *testing.T) *astral.Identity {
	t.Helper()
	subject := astral.GenerateIdentity()
	if err := f.mod.db.StoreExpulsion(sampleSigned(f.userID, subject)); err != nil {
		t.Fatalf("store ban: %v", err)
	}
	return subject
}

// collectBanPushes waits for n pushes, then fails on any further push within quiet.
func collectBanPushes(t *testing.T, p *banPusher, n int, quiet time.Duration) []banPush {
	t.Helper()

	var got []banPush
	for len(got) < n {
		select {
		case push := <-p.pushed:
			got = append(got, push)
		case <-time.After(effectTimeout):
			t.Fatalf("got %d ban pushes %v; want %d", len(got), got, n)
		}
	}

	select {
	case push := <-p.pushed:
		t.Fatalf("pushed the ban on %v to %v; want %d pushes", push.subject, push.target, n)
	case <-time.After(quiet):
	}
	return got
}

// countPushes counts the pushes per target and per (target, subject) pair.
func countPushes(pushes []banPush) (perTarget map[string]int, pairs map[[2]string]int) {
	perTarget = map[string]int{}
	pairs = map[[2]string]int{}
	for _, p := range pushes {
		perTarget[p.target.String()]++
		pairs[[2]string{p.target.String(), p.subject.String()}]++
	}
	return
}

func localLinkEvent(f *receiverFixture, remote *astral.Identity, linkCount int) *recordingDrop {
	event := &events.Event{
		ID:   astral.NewNonce(),
		Data: &nodes.LinkCreatedEvent{RemoteIdentity: remote, LinkCount: astral.Uint32(linkCount)},
	}
	return &recordingDrop{sender: f.nodeID, object: event}
}

// TestLinkCreatedEventSyncsAMemberOnEveryLink: every new link to a member, not
// only the first, schedules one sibling sync.
func TestLinkCreatedEventSyncsAMemberOnEveryLink(t *testing.T) {
	for _, linkCount := range []int{1, 2, 3} {
		f := newReceiverFixture(t, astral.GenerateIdentity())
		pusher := f.withBanPusher()
		sibling := f.addSibling()

		drop := localLinkEvent(f, sibling, linkCount)
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		syncs := f.scheduledSyncs()
		if len(syncs) != 1 || !syncs[0].IsEqual(sibling) {
			t.Fatalf("LinkCount %d scheduled syncs with %v; want one with the sibling %v", linkCount, syncs, sibling)
		}
		if a := drop.accepted(); len(a) != 1 || a[0] {
			t.Fatalf("LinkCount %d: Accept calls %v; want one Accept(false)", linkCount, a)
		}
		collectBanPushes(t, pusher, 0, 50*time.Millisecond)
	}
}

// TestLinkCreatedEventSendsABannedSubjectItsBan: a link from the subject of a
// stored ban pushes the user's bans to it, its own included, under a deadline,
// and schedules no sibling sync.
func TestLinkCreatedEventSendsABannedSubjectItsBan(t *testing.T) {
	for _, linkCount := range []int{1, 2} {
		f := newReceiverFixture(t, astral.GenerateIdentity())
		pusher := f.withBanPusher()
		f.addSibling()
		subject := f.ban(t)
		other := f.ban(t)

		drop := localLinkEvent(f, subject, linkCount)
		if err := f.mod.ReceiveObject(drop); err != nil {
			t.Fatalf("ReceiveObject: %v", err)
		}

		pushes := collectBanPushes(t, pusher, 2, 100*time.Millisecond)
		perTarget, pairs := countPushes(pushes)
		if perTarget[subject.String()] != 2 {
			t.Fatalf("LinkCount %d: pushes %v; want both bans pushed to the subject %v alone", linkCount, pushes, subject)
		}
		if pairs[[2]string{subject.String(), subject.String()}] != 1 {
			t.Fatalf("LinkCount %d: the subject did not get its own ban", linkCount)
		}
		if pairs[[2]string{subject.String(), other.String()}] != 1 {
			t.Fatalf("LinkCount %d: the subject did not get the other ban", linkCount)
		}
		for _, p := range pushes {
			if p.deadline.IsZero() {
				t.Fatal("a ban push has no deadline")
			}
		}
		if syncs := f.scheduledSyncs(); len(syncs) != 0 {
			t.Fatalf("LinkCount %d scheduled syncs with %v; want none for a banned subject", linkCount, syncs)
		}
	}
}

// TestLinkCreatedEventFromAStrangerDoesNothing: a link from a node that is
// neither a member nor banned pushes and schedules nothing.
func TestLinkCreatedEventFromAStrangerDoesNothing(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	pusher := f.withBanPusher()
	f.addSibling()
	f.ban(t)

	drop := localLinkEvent(f, astral.GenerateIdentity(), 1)
	if err := f.mod.ReceiveObject(drop); err != nil {
		t.Fatalf("ReceiveObject: %v", err)
	}

	f.requireNoEffect(t, drop)
	collectBanPushes(t, pusher, 0, 50*time.Millisecond)
}

// TestSyncExpulsionsPushesTheSubjectItsOwnBan: the expulsion sync toward a
// banned subject includes the subject's own ban.
func TestSyncExpulsionsPushesTheSubjectItsOwnBan(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	pusher := f.withBanPusher()
	subject := f.ban(t)

	f.mod.syncExpulsions(astral.NewContext(nil), subject)

	pushes := collectBanPushes(t, pusher, 1, 50*time.Millisecond)
	if !pushes[0].target.IsEqual(subject) || !pushes[0].subject.IsEqual(subject) {
		t.Fatalf("pushed the ban on %v to %v; want the subject's own ban to the subject %v",
			pushes[0].subject, pushes[0].target, subject)
	}
}

// TestReconcileTickPushesBansToLinkedPeers drives the sweep with a fake clock: no
// push before the tick; one tick pushes every stored ban to each linked sibling
// and each linked banned subject, and nothing to unlinked ones.
func TestReconcileTickPushesBansToLinkedPeers(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	pusher := f.withBanPusher()

	linkedSibling, unlinkedSibling := f.addSibling(), f.addSibling()
	linkedSubject, unlinkedSubject := f.ban(t), f.ban(t)
	f.nodes.linked = []*astral.Identity{linkedSibling, linkedSubject}

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

	collectBanPushes(t, pusher, 0, 100*time.Millisecond)

	tick <- time.Now()

	pushes := collectBanPushes(t, pusher, 4, 200*time.Millisecond)
	_, pairs := countPushes(pushes)
	for _, target := range []*astral.Identity{linkedSibling, linkedSubject} {
		for _, subject := range []*astral.Identity{linkedSubject, unlinkedSubject} {
			if pairs[[2]string{target.String(), subject.String()}] != 1 {
				t.Errorf("the ban on %v reached %v %d times; want once", subject, target,
					pairs[[2]string{target.String(), subject.String()}])
			}
		}
	}
	for _, p := range pushes {
		if p.target.IsEqual(unlinkedSibling) || p.target.IsEqual(unlinkedSubject) {
			t.Errorf("pushed to the unlinked %v", p.target)
		}
		if p.deadline.IsZero() {
			t.Error("a sweep push has no deadline")
		}
	}
}

// TestReconcileWithoutActiveContractPushesNothing: a node without an active
// contract has no user bans and no siblings to sweep.
func TestReconcileWithoutActiveContractPushesNothing(t *testing.T) {
	f := newReceiverFixture(t, nil)
	pusher := f.withBanPusher()
	f.nodes.linked = []*astral.Identity{astral.GenerateIdentity()}

	if targets := f.mod.reconcileTargets(); len(targets) != 0 {
		t.Fatalf("sweep targets %v; want none", targets)
	}
	f.mod.reconcileWith(astral.NewContext(nil), f.nodes.linked[0])
	collectBanPushes(t, pusher, 0, 50*time.Millisecond)
}

// TestReconcileInterval_DefaultsToOneHour pins the 1 h default and the rule that a
// zero or negative reconcile_interval selects it.
func TestReconcileInterval_DefaultsToOneHour(t *testing.T) {
	if defaultFileConfig.ReconcileInterval != time.Hour {
		t.Fatalf("default reconcile_interval = %v, want 1h", defaultFileConfig.ReconcileInterval)
	}

	cases := []struct {
		set, want time.Duration
	}{
		{0, time.Hour},
		{-time.Minute, time.Hour},
		{5 * time.Minute, 5 * time.Minute},
	}
	for _, c := range cases {
		mod := &Module{file: FileConfig{ReconcileInterval: c.set}}
		if got := mod.reconcileInterval(); got != c.want {
			t.Errorf("reconcile_interval %v: got %v, want %v", c.set, got, c.want)
		}
	}
}
