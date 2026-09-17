package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("%s did not close within 1s", what)
	}
}

func staysOpen(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s closed, want it still waiting for the pool", what)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLockPoolLocksOnFirstDone(t *testing.T) {
	pool := sig.NewPool()
	pool.Add("a", 1)

	l := LockPool(pool, "a")
	waitClosed(t, l.Done(), "the first locker's Done")

	if l.Done() != l.Done() {
		t.Fatal("Done() returned a different channel on a later call")
	}

	l2 := LockPool(pool, "a")
	staysOpen(t, l2.Done(), "the second locker's Done")

	l.Release()
	waitClosed(t, l2.Done(), "the second locker's Done after Release")
	l2.Release()
}

func TestPoolLockerReleaseUnlocksOnce(t *testing.T) {
	pool := sig.NewPool()
	pool.Add("a", 2)

	a := LockPool(pool, "a")
	b := LockPool(pool, "a")
	waitClosed(t, a.Done(), "A's Done")
	waitClosed(t, b.Done(), "B's Done")

	a.Release()
	a.Release()

	c := LockPool(pool, "a")
	waitClosed(t, c.Done(), "C's Done")

	d := LockPool(pool, "a")
	staysOpen(t, d.Done(), "D's Done")

	c.Release()
	waitClosed(t, d.Done(), "D's Done after C's Release")
	d.Release()
	b.Release()
}

func TestLockPoolWithNoItemsIsDoneAtOnce(t *testing.T) {
	l := LockPool(sig.NewPool())
	waitClosed(t, l.Done(), "Done with no items")
	l.Release()
}

func TestStateString(t *testing.T) {
	for _, c := range []struct {
		state State
		want  string
	}{
		{StateScheduled, "scheduled"},
		{StateRunning, "running"},
		{StateDone, "done"},
		{State(7), "invalid"},
	} {
		if got := c.state.String(); got != c.want {
			t.Errorf("State(%d).String() = %q, want %q", int64(c.state), got, c.want)
		}
	}
}

func TestFuncAdapter(t *testing.T) {
	errRun := errors.New("run failed")
	ctx := astral.NewContext(nil)

	var passed *astral.Context
	task := Func("n", func(c *astral.Context) error {
		passed = c
		return errRun
	})

	if got := task.String(); got != "n" {
		t.Fatalf("String() = %q, want %q", got, "n")
	}
	if err := task.Run(ctx); !errors.Is(err, errRun) {
		t.Fatalf("Run() = %v, want %v", err, errRun)
	}
	if passed != ctx {
		t.Fatal("Run() did not pass its context through to the func")
	}
}
