package user

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// quiet is how long a test waits to see that nothing more happens.
const quiet = 200 * time.Millisecond

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
	return &scriptedPusher{target: target, pushed: make(chan time.Time, 2*pushAttempts), results: results}
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

// stepClock is a backoff clock the test advances: each wait is recorded, and
// it ends when the test calls step.
type stepClock struct {
	waits chan time.Duration
	fire  chan time.Time
}

func newStepClock() *stepClock {
	return &stepClock{waits: make(chan time.Duration, 2*pushAttempts), fire: make(chan time.Time)}
}

func (c *stepClock) after(d time.Duration) <-chan time.Time {
	c.waits <- d
	return c.fire
}

// wait returns the next wait the pusher started.
func (c *stepClock) wait(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.waits:
		return d
	case <-time.After(effectTimeout):
		t.Fatal("the pusher started no wait")
		return 0
	}
}

// step ends the current wait.
func (c *stepClock) step(t *testing.T) {
	t.Helper()
	select {
	case c.fire <- time.Time{}:
	case <-time.After(effectTimeout):
		t.Fatal("the pusher is not waiting")
	}
}

// requirePush waits for one push to the pusher's target and checks it is bounded.
func requirePush(t *testing.T, p *scriptedPusher) {
	t.Helper()
	select {
	case deadline := <-p.pushed:
		if deadline.IsZero() {
			t.Fatal("the push has no deadline")
		}
	case <-time.After(effectTimeout):
		t.Fatal("nothing was pushed to the target")
	}
}

// requireQuiet fails on any further push or wait within quiet.
func requireQuiet(t *testing.T, p *scriptedPusher, c *stepClock) {
	t.Helper()
	select {
	case <-p.pushed:
		t.Fatal("pushed again; want no further push")
	case d := <-c.waits:
		t.Fatalf("started a wait of %v; want none", d)
	case <-time.After(quiet):
	}
}

// signingCrypto signs with the marker signature the fixtures accept.
type signingCrypto struct{ markerCrypto }

func (signingCrypto) Sign(*astral.Context, *crypto.PublicKey, crypto.SignableTextObject) (*crypto.Signature, error) {
	return validSig(), nil
}

// pusherModule builds a module whose pushes go to p and whose backoff runs on c.
func pusherModule(ctx *astral.Context, p *scriptedPusher, c *stepClock) *Module {
	id := astral.GenerateIdentity()
	mod := &Module{ctx: ctx, node: &identityNode{id: id}, log: log.New(id), after: c.after}
	mod.Deps.Objects = p
	return mod
}

// runPush runs pushUntilDelivered to p's target and returns a channel closed when it returns.
func runPush(mod *Module, ctx *astral.Context, p *scriptedPusher) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		mod.pushUntilDelivered(ctx, p.target, &astral.EOS{})
	}()
	return done
}

func requireDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(effectTimeout):
		t.Fatal("pushUntilDelivered did not return")
	}
}

// TestExpelRetriesThePushToTheSubjectUntilDelivered: the subject's ban push
// fails twice and then succeeds; Expel pushes it three times, 1 s and 2 s
// apart, and then stops.
func TestExpelRetriesThePushToTheSubjectUntilDelivered(t *testing.T) {
	f := newReceiverFixture(t, astral.GenerateIdentity())
	subject := astral.GenerateIdentity()
	pusher := newScriptedPusher(subject, errUnreachable, errUnreachable)
	clock := newStepClock()
	f.mod.Deps.Crypto = signingCrypto{}
	f.mod.Deps.Objects = pusher
	f.mod.after = clock.after

	if _, err := f.mod.Expel(astral.NewContext(nil), subject); err != nil {
		t.Fatalf("Expel: %v", err)
	}

	for _, want := range []time.Duration{time.Second, 2 * time.Second} {
		requirePush(t, pusher)
		if got := clock.wait(t); got != want {
			t.Fatalf("waited %v before the next push; want %v", got, want)
		}
		clock.step(t)
	}
	requirePush(t, pusher)
	requireQuiet(t, pusher, clock)
}

// TestPushUntilDeliveredStopsWhenTheContextEnds: a push that keeps failing
// stops when the module context ends during the wait.
func TestPushUntilDeliveredStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := astral.NewContext(nil).WithCancel()
	defer cancel()
	pusher := newScriptedPusher(astral.GenerateIdentity(), errUnreachable, errUnreachable)
	clock := newStepClock()
	mod := pusherModule(ctx, pusher, clock)

	done := runPush(mod, ctx, pusher)
	requirePush(t, pusher)
	clock.wait(t)
	cancel()

	requireDone(t, done)
	requireQuiet(t, pusher, clock)
}

// TestPushUntilDeliveredStopsOnARejection: a target that received the object
// and refused it is not asked again.
func TestPushUntilDeliveredStopsOnARejection(t *testing.T) {
	for _, rejection := range []error{objectsapi.ErrPushRejected, objectsmod.ErrPushRejected} {
		pusher := newScriptedPusher(astral.GenerateIdentity(), rejection)
		clock := newStepClock()
		ctx := astral.NewContext(nil)
		mod := pusherModule(ctx, pusher, clock)

		done := runPush(mod, ctx, pusher)
		requirePush(t, pusher)
		requireDone(t, done)
		requireQuiet(t, pusher, clock)
	}
}

// TestPushUntilDeliveredGivesUpAfterMaxAttempts: a target that is never
// reached gets pushAttempts pushes on a backoff that doubles from 1 s up to
// 15 min, and then no more.
func TestPushUntilDeliveredGivesUpAfterMaxAttempts(t *testing.T) {
	failures := make([]error, pushAttempts+1)
	for i := range failures {
		failures[i] = errUnreachable
	}
	pusher := newScriptedPusher(astral.GenerateIdentity(), failures...)
	clock := newStepClock()
	ctx := astral.NewContext(nil)
	mod := pusherModule(ctx, pusher, clock)

	done := runPush(mod, ctx, pusher)
	want := time.Second
	for attempt := 1; attempt < pushAttempts; attempt++ {
		requirePush(t, pusher)
		if got := clock.wait(t); got != want {
			t.Fatalf("waited %v after attempt %d; want %v", got, attempt, want)
		}
		clock.step(t)
		want = min(want*2, 15*time.Minute)
	}
	requirePush(t, pusher)

	requireDone(t, done)
	requireQuiet(t, pusher, clock)
}
