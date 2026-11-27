package user

import (
	"errors"
	"sync"
	"testing"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// quiet is how long a test waits to see that nothing more happens.
const quiet = 200 * time.Millisecond

// testBackoff stands in for siblingPushBackoff in the tests that wait for a
// retry: short enough to run in real time.
var testBackoff = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}

var errUnreachable = errors.New("route not found")

// scriptedPusher is an objects module that answers the pushes to target with
// results in order, and with nil after the last one. It records the deadline
// of each push to target and ignores pushes to any other node.
type scriptedPusher struct {
	objectsmod.Module
	target *astral.Identity
	pushed chan time.Time

	mu      sync.Mutex
	results []error
}

func newScriptedPusher(target *astral.Identity, results ...error) *scriptedPusher {
	return &scriptedPusher{target: target, pushed: make(chan time.Time, 64), results: results}
}

func (p *scriptedPusher) Push(ctx *astral.Context, target *astral.Identity, _ astral.Object) error {
	if !target.IsEqual(p.target) {
		return nil
	}
	deadline, _ := ctx.Deadline()
	p.pushed <- deadline

	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.results) == 0 {
		return nil
	}
	err := p.results[0]
	p.results = p.results[1:]
	return err
}

// requirePush waits for one push to the pusher's target and checks it is bounded.
// It returns the push's deadline, which orders the pushes in time.
func requirePush(t *testing.T, p *scriptedPusher) time.Time {
	t.Helper()
	select {
	case deadline := <-p.pushed:
		if deadline.IsZero() {
			t.Fatal("the push has no deadline")
		}
		return deadline
	case <-time.After(effectTimeout):
		t.Fatal("nothing was pushed to the target")
		return time.Time{}
	}
}

// requireQuiet fails on any further push within quiet.
func requireQuiet(t *testing.T, p *scriptedPusher) {
	t.Helper()
	select {
	case <-p.pushed:
		t.Fatal("pushed again; want no further push")
	case <-time.After(quiet):
	}
}

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
// pusher scripted with results.
func retryFixture(t *testing.T, results ...error) (mod *Module, sib *astral.Identity, pusher *objectPusher, nodes *linkedNodes) {
	t.Helper()

	sib = astral.GenerateIdentity()
	mod = pushFixture(t, []*astral.Identity{sib}, []*astral.Identity{sib})
	pusher = newObjectPusher(sib, results...)
	mod.Deps.Objects = pusher
	return mod, sib, pusher, mod.Deps.Nodes.(*linkedNodes)
}

// pushWithRetries starts the retried push of obj to every linked sibling of
// mod on testBackoff.
func pushWithRetries(mod *Module, ctx *astral.Context, obj astral.Object) {
	for _, sib := range mod.getSiblings() {
		go mod.pushToSiblingRetry(ctx, sib, obj, testBackoff)
	}
}

// TestSiblingPushBackoff_Values pins the waits before the retries of a push.
func TestSiblingPushBackoff_Values(t *testing.T) {
	want := []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}
	if len(siblingPushBackoff) != len(want) {
		t.Fatalf("siblingPushBackoff = %v; want %v", siblingPushBackoff, want)
	}
	for i := range want {
		if siblingPushBackoff[i] != want[i] {
			t.Fatalf("siblingPushBackoff = %v; want %v", siblingPushBackoff, want)
		}
	}
}

// TestPushToSiblingsRetry_DeliversOnThirdAttempt: the first two pushes of an
// object to B fail; the push is repeated after the first two backoff waits,
// delivers the same object on the third attempt, and stops.
func TestPushToSiblingsRetry_DeliversOnThirdAttempt(t *testing.T) {
	mod, _, pusher, _ := retryFixture(t, errUnreachable, errUnreachable)
	contract := relayForContract(astral.GenerateIdentity(), mod.node.Identity())

	pushWithRetries(mod, astral.NewContext(nil), contract)

	var at []time.Time
	for range 3 {
		at = append(at, requirePush(t, pusher.scriptedPusher))
	}
	for i, want := range testBackoff[:2] {
		if got := at[i+1].Sub(at[i]); got < want {
			t.Fatalf("push %d came %v after the one before; want at least %v", i+2, got, want)
		}
	}
	requireQuiet(t, pusher.scriptedPusher)

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
	mod, _, pusher, _ := retryFixture(t)

	mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

	requirePush(t, pusher.scriptedPusher)
	requireQuiet(t, pusher.scriptedPusher)
}

// TestPushToSiblingsRetry_StopsOnRejected: a sibling that received the object
// and refused it, whether through the wire error or the local one, is not asked
// again.
func TestPushToSiblingsRetry_StopsOnRejected(t *testing.T) {
	for _, rejection := range []error{objectsapi.ErrPushRejected, objectsmod.ErrPushRejected} {
		mod, _, pusher, _ := retryFixture(t, rejection)

		mod.PushToSiblingsRetry(astral.NewContext(nil), &astral.EOS{})

		requirePush(t, pusher.scriptedPusher)
		requireQuiet(t, pusher.scriptedPusher)
	}
}

// TestPushToSiblingsRetry_GivesUpAfterFourPushes: a push that always fails is
// made exactly 4 times, one backoff wait apart.
func TestPushToSiblingsRetry_GivesUpAfterFourPushes(t *testing.T) {
	failures := make([]error, 8)
	for i := range failures {
		failures[i] = errUnreachable
	}
	mod, _, pusher, _ := retryFixture(t, failures...)

	pushWithRetries(mod, astral.NewContext(nil), &astral.EOS{})

	var at []time.Time
	for range 4 {
		at = append(at, requirePush(t, pusher.scriptedPusher))
	}
	for i, want := range testBackoff {
		if got := at[i+1].Sub(at[i]); got < want {
			t.Fatalf("push %d came %v after the one before; want at least %v", i+2, got, want)
		}
	}
	requireQuiet(t, pusher.scriptedPusher)

	if n := len(pusher.pushedObjects()); n != 4 {
		t.Fatalf("%d pushes reached the sibling; want exactly 4", n)
	}
}

// leavingPusher drops the sibling's link during the first push to it and fails
// the push.
type leavingPusher struct {
	*objectPusher
	nodes *linkedNodes
}

func (p *leavingPusher) Push(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	// note: Push and the check after the wait run on the one goroutine of the
	// sibling's retries, so the write needs no lock.
	p.nodes.linked = nil
	return p.objectPusher.Push(ctx, target, obj)
}

// TestPushToSiblingsRetry_StopsWhenTheSiblingLeaves: a sibling that is no
// longer linked when its wait ends gets no further push.
func TestPushToSiblingsRetry_StopsWhenTheSiblingLeaves(t *testing.T) {
	mod, _, pusher, nodes := retryFixture(t, errUnreachable, errUnreachable, errUnreachable)
	mod.Deps.Objects = &leavingPusher{objectPusher: pusher, nodes: nodes}

	pushWithRetries(mod, astral.NewContext(nil), &astral.EOS{})

	requirePush(t, pusher.scriptedPusher)
	requireQuiet(t, pusher.scriptedPusher)
}

// TestPushToSiblingsRetry_StopsWhenTheContextEnds: a push that keeps failing
// stops when its context ends during a wait.
func TestPushToSiblingsRetry_StopsWhenTheContextEnds(t *testing.T) {
	mod, _, pusher, _ := retryFixture(t, errUnreachable, errUnreachable)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()

	// the wait is long enough that only the cancel below ends it
	for _, sib := range mod.getSiblings() {
		go mod.pushToSiblingRetry(ctx, sib, &astral.EOS{}, []time.Duration{time.Hour})
	}

	requirePush(t, pusher.scriptedPusher)
	cancel()

	requireQuiet(t, pusher.scriptedPusher)
}

// TestPushToSiblingsRetry_RetriesEachSiblingOnItsOwn: a sibling that fails does
// not delay or repeat the push to one that delivers.
func TestPushToSiblingsRetry_RetriesEachSiblingOnItsOwn(t *testing.T) {
	flaky, steady := astral.GenerateIdentity(), astral.GenerateIdentity()
	mod := pushFixture(t, []*astral.Identity{flaky, steady}, []*astral.Identity{flaky, steady})
	flakyPusher := newObjectPusher(flaky, errUnreachable)
	both := &routedPushers{flaky: flakyPusher, steadyID: steady, steadyPushed: make(chan struct{}, 8)}
	mod.Deps.Objects = both

	pushWithRetries(mod, astral.NewContext(nil), &astral.EOS{})

	requirePush(t, flakyPusher.scriptedPusher)
	select {
	case <-both.steadyPushed:
	case <-time.After(effectTimeout):
		t.Fatal("the steady sibling got no push while the flaky one waited")
	}
	requirePush(t, flakyPusher.scriptedPusher)
	requireQuiet(t, flakyPusher.scriptedPusher)

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
