package user

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type fakeHead struct{ head atomic.Uint64 }

func (h *fakeHead) SwarmHead() uint64 { return h.head.Load() }

// TestSwarmWatcherReportsHeadChanges: the watcher reports a non-empty set at
// start and each grown head once, and nothing while the head stays put.
func TestSwarmWatcherReportsHeadChanges(t *testing.T) {
	state := &fakeHead{}
	state.head.Store(2)
	seen := make(chan uint64, 16)
	w := newSwarmWatcher(state, time.Millisecond, func(head uint64) { seen <- head })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	want := func(head uint64) {
		t.Helper()
		select {
		case got := <-seen:
			if got != head {
				t.Fatalf("onChange(%d), want %d", got, head)
			}
		case <-time.After(time.Second):
			t.Fatalf("no onChange(%d)", head)
		}
	}
	want(2)
	time.Sleep(20 * time.Millisecond)
	if len(seen) != 0 {
		t.Fatalf("onChange called %d times with an unchanged head", len(seen))
	}
	state.head.Store(5)
	want(5)

	cancel()
	if err := <-done; err != context.Canceled {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
}
