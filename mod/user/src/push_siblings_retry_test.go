package user

import (
	"sync"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// objectPusher is a scriptedPusher that also records the object of each push.
type objectPusher struct {
	*scriptedPusher

	mu   sync.Mutex
	objs []astral.Object
}

func newObjectPusher(target *astral.Identity, results ...error) *objectPusher {
	return &objectPusher{scriptedPusher: newScriptedPusher(target, results...)}
}

func (p *objectPusher) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	if target.IsEqual(p.target) {
		p.mu.Lock()
		p.objs = append(p.objs, obj)
		p.mu.Unlock()
	}
	return p.scriptedPusher.Push(ctx, target, obj)
}

func (p *objectPusher) pushedObjects() []astral.Object {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]astral.Object(nil), p.objs...)
}

// retryFixture builds a module with one linked sibling B, whose pushes go to a
// pusher scripted with results and whose backoff runs on a step clock.
func retryFixture(t *testing.T, results ...error) (mod *Module, sib *astral.Identity, pusher *objectPusher, clock *stepClock, nodes *linkedNodes) {
	t.Helper()

	sib = astral.GenerateIdentity()
	mod = pushFixture(t, []*astral.Identity{sib}, []*astral.Identity{sib})
	pusher = newObjectPusher(sib, results...)
	clock = newStepClock()
	mod.Deps.Objects = pusher
	mod.after = clock.after
	return mod, sib, pusher, clock, mod.Deps.Nodes.(*linkedNodes)
}

// TestPushToSiblingsRetry_DeliversOnThirdAttempt: the first two pushes of an
// object to B fail; the push is repeated after the first two backoff waits,
// delivers the same object on the third attempt, and stops.
func TestPushToSiblingsRetry_DeliversOnThirdAttempt(t *testing.T) {
	mod, _, pusher, clock, _ := retryFixture(t, errUnreachable, errUnreachable)
	contract := relayForContract(astral.GenerateIdentity(), mod.node.Identity())

	mod.PushToSiblingsRetry(astral.NewContext(nil), contract)

	for _, want := range []time.Duration{10 * time.Second, time.Minute} {
		requirePush(t, pusher.scriptedPusher)
		if got := clock.wait(t); got != want {
			t.Fatalf("waited %v before the next push; want %v", got, want)
		}
		clock.step(t)
	}
	requirePush(t, pusher.scriptedPusher)
	requireQuiet(t, pusher.scriptedPusher, clock)

	objs := pusher.pushedObjects()
	if len(objs) != 3 {
		t.Fatalf("%d pushes reached the sibling; want 3", len(objs))
	}
	for i, obj := range objs {
		if obj != astral.Object(contract) {
			t.Fatalf("push %d carried %v; want the pushed contract", i+1, obj)
		}
	}
}

// TestPushToSiblingsRetry_PushesOnceWhenTheFirstPushDelivers: a push that
// reaches the sibling starts no wait and no retry.
func TestPushToSiblingsRetry_PushesOnceWhenTheFirstPushDelivers(t *testing.T) {
	mod, _, pusher, clock, _ := retryFixture(t)

	mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

	requirePush(t, pusher.scriptedPusher)
	requireQuiet(t, pusher.scriptedPusher, clock)
}

// TestPushToSiblingsRetry_StopsOnRejected: a sibling that received the object
// and refused it, whether through the wire error or the local one, is not asked
// again.
func TestPushToSiblingsRetry_StopsOnRejected(t *testing.T) {
	for _, rejection := range []error{objectsapi.ErrPushRejected, objectsmod.ErrPushRejected} {
		mod, _, pusher, clock, _ := retryFixture(t, rejection)

		mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

		requirePush(t, pusher.scriptedPusher)
		requireQuiet(t, pusher.scriptedPusher, clock)
	}
}

// TestPushToSiblingsRetry_GivesUpAfterFourPushes: a push that always fails is
// made exactly 4 times, 10 s, 1 min and 5 min apart.
func TestPushToSiblingsRetry_GivesUpAfterFourPushes(t *testing.T) {
	failures := make([]error, 8)
	for i := range failures {
		failures[i] = errUnreachable
	}
	mod, _, pusher, clock, _ := retryFixture(t, failures...)

	mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

	for _, want := range []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute} {
		requirePush(t, pusher.scriptedPusher)
		if got := clock.wait(t); got != want {
			t.Fatalf("waited %v before the next push; want %v", got, want)
		}
		clock.step(t)
	}
	requirePush(t, pusher.scriptedPusher)
	requireQuiet(t, pusher.scriptedPusher, clock)

	if n := len(pusher.pushedObjects()); n != 4 {
		t.Fatalf("%d pushes reached the sibling; want exactly 4", n)
	}
}

// TestPushToSiblingsRetry_StopsWhenTheSiblingLeaves: a sibling that is no
// longer linked when its wait ends gets no further push.
func TestPushToSiblingsRetry_StopsWhenTheSiblingLeaves(t *testing.T) {
	mod, _, pusher, clock, nodes := retryFixture(t, errUnreachable, errUnreachable, errUnreachable)

	mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

	requirePush(t, pusher.scriptedPusher)
	clock.wait(t)
	// note: the pusher reads the links only after the wait ends, which the
	// step below orders after this write.
	nodes.linked = nil
	clock.step(t)

	requireQuiet(t, pusher.scriptedPusher, clock)
}

// TestPushToSiblingsRetry_StopsWhenTheContextEnds: a push that keeps failing
// stops when its context ends during a wait.
func TestPushToSiblingsRetry_StopsWhenTheContextEnds(t *testing.T) {
	mod, _, pusher, clock, _ := retryFixture(t, errUnreachable, errUnreachable)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	mod.PushToSiblingsRetry(ctx, &astral.EOS{})

	requirePush(t, pusher.scriptedPusher)
	clock.wait(t)
	cancel()

	requireQuiet(t, pusher.scriptedPusher, clock)
}

// TestPushToSiblingsRetry_RetriesEachSiblingOnItsOwn: a sibling that fails does
// not delay or repeat the push to one that delivers.
func TestPushToSiblingsRetry_RetriesEachSiblingOnItsOwn(t *testing.T) {
	flaky, steady := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := pushFixture(t, []*astral.Identity{flaky, steady}, []*astral.Identity{flaky, steady})
	flakyPusher := newObjectPusher(flaky, errUnreachable)
	both := &routedPushers{flaky: flakyPusher, steadyID: steady, steadyPushed: make(chan struct{}, 8)}
	clock := newStepClock()
	mod.Deps.Objects = both
	mod.after = clock.after

	mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

	requirePush(t, flakyPusher.scriptedPusher)
	clock.wait(t)
	select {
	case <-both.steadyPushed:
	case <-time.After(effectTimeout):
		t.Fatal("the steady sibling got no push while the flaky one waited")
	}
	clock.step(t)
	requirePush(t, flakyPusher.scriptedPusher)
	requireQuiet(t, flakyPusher.scriptedPusher, clock)

	if n := len(both.steadyPushed); n != 0 {
		t.Fatalf("the steady sibling got %d more pushes; want none", n)
	}
}

// routedPushers answers the pushes to one sibling with a scripted pusher and
// every push to steadyID with success.
type routedPushers struct {
	objectsmod.Module
	flaky        *objectPusher
	steadyID     *astral.Identity
	steadyPushed chan struct{}
}

func (r *routedPushers) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	if target.IsEqual(r.steadyID) {
		r.steadyPushed <- struct{}{}
		return nil
	}
	return r.flaky.Push(ctx, target, obj)
}
