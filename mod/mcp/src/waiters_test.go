package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// The invariant the floor exists for.

// A missed wake must be caught before the deadline can report timed_out over a
// non-empty inbox. That answer — "the window closed with nothing new", while
// unarchived mail sits in the inbox — is the one this design must never give.
func TestTheFloorIsUnderTheDeadline(t *testing.T) {
	if waitFloor >= defaultConfig.WaitDefault {
		t.Fatalf("floor %v is not under the default window %v: a missed wake would be reported as timed_out",
			waitFloor, defaultConfig.WaitDefault)
	}
}

// The wake.

// A delivery wakes the park that is watching that mailbox, and it does so well
// inside the floor — otherwise the wake is doing nothing and the floor is.
func TestADeliveryWakesTheParkedWait(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	type answer struct {
		rows []*mcp.StoredMessage
		took time.Duration
	}
	done := make(chan answer, 1)
	go func() {
		start := time.Now()
		ans, err := mod.waitMessages(context.Background(), a, waitRequest{})
		if err != nil {
			t.Error(err)
		}
		done <- answer{ans.Rows, time.Since(start)}
	}()

	waitUntilParked(t, mod, a, 1)

	if err := mod.storeMessage(b, a, &mcp.Message{ID: mcp.NewMessageID(), Content: "x"}); err != nil {
		t.Fatalf("store: %v", err)
	}

	select {
	case got := <-done:
		if len(got.rows) != 1 {
			t.Fatalf("the wake answered %v rows", len(got.rows))
		}
		if got.took > waitFloor/2 {
			t.Fatalf("answered in %v — the floor did this, not the wake", got.took)
		}
		t.Logf("woken in %v (floor is %v)", got.took.Round(time.Millisecond), waitFloor)
	case <-time.After(5 * time.Second):
		t.Fatal("a delivery did not wake the park")
	}
}

// Every parked session under one identity is woken, because the endpoint keys
// no session by identity and each holds its own channel.
func TestEverySessionUnderOneIdentityIsWoken(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	const sessions = 3
	done := make(chan int, sessions)
	for range sessions {
		go func() {
			ans, _ := mod.waitMessages(context.Background(), a, waitRequest{})
			done <- len(ans.Rows)
		}()
	}
	waitUntilParked(t, mod, a, sessions)

	if err := mod.storeMessage(b, a, &mcp.Message{ID: mcp.NewMessageID(), Content: "x"}); err != nil {
		t.Fatal(err)
	}

	for range sessions {
		select {
		case n := <-done:
			if n != 1 {
				t.Fatalf("a session answered %v rows; the park takes nothing from the others", n)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("not every session was woken")
		}
	}
}

// A wake names one mailbox. Waking every parked agent would cost a query each,
// measured at two hundred to one.
func TestAWakeReachesOneMailbox(t *testing.T) {
	var w waiters
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	wokeA, leaveA := w.park(a)
	defer leaveA()
	wokeB, leaveB := w.park(b)
	defer leaveB()

	w.wake(a)

	select {
	case <-wokeA:
	default:
		t.Fatal("the owner's own waiter was not woken")
	}
	select {
	case <-wokeB:
		t.Fatal("a wake for one mailbox reached another")
	default:
	}
}

// undo puts a row back into the wait set with no insert, so it is the one
// statement besides a delivery that adds to what a park is watching.
func TestUndoingAnArchiveWakesTheWait(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	id := mcp.NewMessageID()
	mustInsertInbox(t, mod, &mcp.StoredMessage{ID: id, Sender: b, Recipient: a, Content: "x"})
	if _, err := mod.archiveMessage(a, messageRef{Box: mcp.BoxInbox, ID: id}, false); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() {
		ans, _ := mod.waitMessages(context.Background(), a, waitRequest{})
		done <- len(ans.Rows)
	}()
	waitUntilParked(t, mod, a, 1)

	if _, err := mod.archiveMessage(a, messageRef{Box: mcp.BoxInbox, ID: id}, true); err != nil {
		t.Fatal(err)
	}

	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("undo answered %v rows", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("undo did not wake the park")
	}
}

// A delivery that stored nothing has nothing to wake anyone about. storeMessage
// returns nil both when it wrote the row and when it recognised an honest
// retry, so the wake hangs off the affected-row count and not off the error.
func TestARetryThatStoredNothingWakesNobody(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	msg := &mcp.Message{ID: mcp.NewMessageID(), Content: "x"}

	if err := mod.storeMessage(b, a, msg); err != nil {
		t.Fatal(err)
	}
	if _, err := mod.archiveMessage(a, messageRef{Box: mcp.BoxInbox, ID: msg.ID}, false); err != nil {
		t.Fatal(err)
	}

	woke, leave := mod.waiters.park(a)
	defer leave()

	// the same sender delivering the same id again: an honest retry, nil error,
	// nothing written
	if err := mod.storeMessage(b, a, msg); err != nil {
		t.Fatalf("a retry must not fail: %v", err)
	}

	select {
	case <-woke:
		t.Fatal("a delivery that wrote no row woke a waiter")
	default:
	}
}

// The registry keeps no more than it is holding.

// A registration that outlives its park is a leak with no other symptom: one
// entry per parked agent, forever.
func TestEveryExitPathUnregisters(t *testing.T) {
	mod := testMessageModule(t)
	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()

	t.Run("answered", func(t *testing.T) {
		mustInsertInbox(t, mod, &mcp.StoredMessage{ID: mcp.NewMessageID(), Sender: b, Recipient: a, Content: "x"})
		if _, err := mod.waitMessages(context.Background(), a, waitRequest{}); err != nil {
			t.Fatal(err)
		}
		if n := mod.waiters.parked(a); n != 0 {
			t.Fatalf("%v waiters left after an answered park", n)
		}
	})

	c := astral.GenerateIdentity()

	t.Run("timed out", func(t *testing.T) {
		if _, err := mod.waitMessages(context.Background(), c, waitRequest{Timeout: 20 * time.Millisecond}); err != nil {
			t.Fatal(err)
		}
		if n := mod.waiters.parked(c); n != 0 {
			t.Fatalf("%v waiters left after a deadline", n)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			mod.waitMessages(ctx, c, waitRequest{})
		}()
		waitUntilParked(t, mod, c, 1)
		cancel()
		<-done
		if n := mod.waiters.parked(c); n != 0 {
			t.Fatalf("%v waiters left after a cancellation", n)
		}
	})

	t.Run("refused before parking", func(t *testing.T) {
		if _, err := mod.waitMessages(context.Background(), c, waitRequest{Since: "not a cursor"}); err == nil {
			t.Fatal("a bad cursor must be refused")
		}
		if n := mod.waiters.parked(c); n != 0 {
			t.Fatalf("%v waiters left after a refusal", n)
		}
	})
}

// The wake fires from a writer that still owes its sender an Ack. If it could
// block, that ack would stall until the sender's query timeout closed the conn,
// and the sender would record a stored message as one whose fate is unknown.
func TestAWakeNeverBlocksItsWriter(t *testing.T) {
	var w waiters
	a := astral.GenerateIdentity()

	_, leave := w.park(a) // nobody ever receives
	defer leave()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			w.wake(a) // the buffer fills on the first and stays full
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a wake blocked on a waiter that was not receiving")
	}
}

// The registry is reached from a delivery goroutine and from every parked wait.
func TestTheRegistryIsSafeUnderConcurrency(t *testing.T) {
	var w waiters
	ids := make([]*astral.Identity, 8)
	for i := range ids {
		ids[i] = astral.GenerateIdentity()
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		for range 25 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, leave := w.park(id)
				w.wake(id)
				leave()
			}()
		}
	}
	wg.Wait()

	for _, id := range ids {
		if n := w.parked(id); n != 0 {
			t.Fatalf("%v waiters left registered", n)
		}
	}
}

// waitUntilParked blocks until the owner has n registered waiters, so a test
// never races the goroutine it just started.
func waitUntilParked(t *testing.T, mod *Module, owner *astral.Identity, n int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mod.waiters.parked(owner) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("only %v of %v waiters parked", mod.waiters.parked(owner), n)
}
