package user

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// publishLog records the events a publisher emits, as "<type> <id>".
type publishLog struct {
	mu     sync.Mutex
	events []string
}

func (l *publishLog) emit(e astral.Object) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, eventLine(e))
}

// take returns the events since the last take.
func (l *publishLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.events
	l.events = nil
	return out
}

func eventLine(e astral.Object) string {
	switch e := e.(type) {
	case *user.SwarmRequestFiled:
		return "filed " + e.RequestID.String()
	case *user.SwarmRequestApproved:
		return "approved " + e.RequestID.String() + " by " + e.ApprovalID.String()
	case *user.SwarmRequestDeclined:
		return "declined " + e.RequestID.String()
	case *user.SwarmRequestExpired:
		return "expired " + e.RequestID.String()
	case *user.SwarmContractAdded:
		return "contract " + e.ContractID.String()
	}
	return fmt.Sprintf("unexpected %v", e.ObjectType())
}

func newTestPublisher(f *viewFixture) (*swarmPublisher, *publishLog) {
	log := &publishLog{}
	return newSwarmPublisher(f.view.snapshot, log.emit), log
}

func wantEvents(t *testing.T, log *publishLog, want ...string) {
	t.Helper()
	if got := log.take(); !slices.Equal(got, want) {
		t.Fatalf("events %q; want %q", got, want)
	}
}

// TestSwarmPublisherEmitsOncePerVerdictChange (criterion 5): each event fires
// once per verdict change and never again for an unchanged view; nothing is
// emitted for what was stored before record.
func TestSwarmPublisherEmitsOncePerVerdictChange(t *testing.T) {
	f := newViewFixture(t)
	old := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	p, log := newTestPublisher(f)
	p.publish()
	p.record()
	wantEvents(t, log)

	r := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 1)))
	p.publish()
	p.publish()
	wantEvents(t, log, "filed "+r.String())

	a := f.add(f.approval(f.laptop, r))
	p.publish()
	p.publish()
	wantEvents(t, log, "approved "+r.String()+" by "+a.String())

	c := f.add(f.appContract(f.pi, f.appA, nil))
	p.publish()
	p.publish()
	wantEvents(t, log, "contract "+c.String())

	f.add(f.decline(f.desktop, r))
	p.publish()
	p.publish()
	wantEvents(t, log, "declined "+r.String())

	f.now = viewClock(11, 1)
	p.publish()
	p.publish()
	wantEvents(t, log, "expired "+old.String())
}

// TestSwarmPublisherSkipsVerdictsThatDoNotCount (criterion 5): no event fires
// for an Approval whose approver is expelled, nor for one a Decline
// overrides, nor for a contract its subject did not sign or whose issuer is
// expelled.
func TestSwarmPublisherSkipsVerdictsThatDoNotCount(t *testing.T) {
	f := newViewFixture(t)
	f.add(f.expulsion(f.user, f.desktop))
	p, log := newTestPublisher(f)
	p.record()

	byExpelled := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	f.add(f.approval(f.desktop, byExpelled))
	f.add(f.appContract(f.desktop, f.appA, nil))
	p.publish()
	wantEvents(t, log, "filed "+byExpelled.String())

	overridden := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 1)))
	f.add(f.approval(f.laptop, overridden))
	f.add(f.decline(f.phone, overridden))
	half := f.appContract(f.pi, f.appA, overridden)
	half.SubjectSig = nil
	f.add(half)
	p.publish()
	wantEvents(t, log, "filed "+overridden.String(), "declined "+overridden.String())
}

// scriptedFeed is a SwarmFeed whose changes a test sends.
type scriptedFeed struct{ changes chan SwarmChange }

func (f scriptedFeed) Follow(*astral.Context) <-chan SwarmChange { return f.changes }

// TestSwarmPublisherFollowsAScriptedFeed (criterion 6): the publisher runs
// against any SwarmFeed: each change the feed reports runs the change hook
// and publishes, and the publisher stops when the feed ends.
func TestSwarmPublisherFollowsAScriptedFeed(t *testing.T) {
	f := newViewFixture(t)
	p, log := newTestPublisher(f)
	p.record()
	feed := scriptedFeed{changes: make(chan SwarmChange)}
	changed := make(chan struct{}, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.follow(astral.NewContext(nil), feed, func() { changed <- struct{}{} })
	}()

	r := f.add(f.contractRequest(f.pi, f.appA, viewClock(10, 0)))
	feed.changes <- SwarmChange{}
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("the change hook did not run")
	}
	awaitEvents(t, log, "filed "+r.String())

	close(feed.changes)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the publisher did not stop when the feed ended")
	}
}

// awaitEvents waits until log holds want.
func awaitEvents(t *testing.T, log *publishLog, want ...string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []string
	for time.Now().Before(deadline) {
		got = append(got, log.take()...)
		if len(got) >= len(want) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("events %q; want %q", got, want)
	}
}

// TestSwarmMergePublishesAtOnce: an entry this node merges is published by the
// merge itself, before any feed reports it.
func TestSwarmMergePublishesAtOnce(t *testing.T) {
	n := newStateNode(t, newSwarmScript())
	root := astral.GenerateIdentity()
	view := newSwarmView(n.store, func() *astral.Identity { return root }, nil)
	log := &publishLog{}
	p := newSwarmPublisher(view.snapshot, log.emit)
	p.record()
	n.store.stored = func([]astral.Object) { p.publish() }

	req := &user.SignedContractRequest{
		ContractRequest: user.NewContractRequest(root, astral.GenerateIdentity(), nil),
		FiledBySig:      scriptedSig(),
	}
	mustAdd(t, n, req)
	id := entryID(t, req)
	wantEvents(t, log, "filed "+id.String())
}
