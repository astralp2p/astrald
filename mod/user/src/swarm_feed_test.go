package user

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// TestHeadFeedReportsHeadChanges: the head feed reports a non-empty set at
// start and each grown head once, nothing while the head stays put, and
// closes its channel when ctx ends.
func TestHeadFeedReportsHeadChanges(t *testing.T) {
	var head atomic.Uint64
	head.Store(2)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	changes := (&headFeed{head: head.Load, interval: time.Millisecond}).Follow(ctx)

	want := func() {
		t.Helper()
		select {
		case c, ok := <-changes:
			if !ok || c.IDs != nil {
				t.Fatalf("change %v, open %v; want a re-read", c, ok)
			}
		case <-time.After(time.Second):
			t.Fatal("no change reported")
		}
	}
	want()
	select {
	case <-changes:
		t.Fatal("a change reported with an unchanged head")
	case <-time.After(20 * time.Millisecond):
	}
	head.Store(5)
	want()

	cancel()
	select {
	case _, ok := <-changes:
		if ok {
			t.Fatal("a change after ctx ended")
		}
	case <-time.After(time.Second):
		t.Fatal("the feed did not close when ctx ended")
	}
}
