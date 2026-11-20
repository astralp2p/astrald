package nodes

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

const (
	// wakeIdle is the idle ping of the links in these tests: far past any wait in them.
	wakeIdle = 10 * time.Second
	// wakeTimeout is their ping timeout.
	wakeTimeout = 300 * time.Millisecond
	// wakeDeadline is the deadline of the queries sent over them.
	wakeDeadline = 200 * time.Millisecond
	// wakeSlack is how late past its bound an effect may land.
	wakeSlack = 500 * time.Millisecond
)

// newWakeLink starts a link over a pipe with the idle ping wakeIdle and the
// ping timeout wakeTimeout, whose ping loop has left its start jitter, and
// starts its peer. A peer that answers is healthy; one that does not swallows
// every frame.
func newWakeLink(t *testing.T, answer bool) (*Link, *probePeer) {
	t.Helper()
	mod := &Module{log: log.New(astral.Anyone)}
	link, remote := newKeepaliveLink(t, mod, wakeIdle, wakeTimeout)
	t.Cleanup(func() { link.Close() })
	peer := startProbePeer(t, remote, answer)
	settle()
	return link, peer
}

// routeOver routes one query over link under ctx and returns its error.
func routeOver(link *Link, ctx *astral.Context) error {
	q := outcomeQuery(astral.Anyone, astral.Anyone)
	_, err := link.RouteQuery(ctx, q, nopWriteCloser{})
	return err
}

// waitProbes polls peer until it has received n probes or within passes, and returns the count.
func waitProbes(peer *probePeer, n int, within time.Duration) int {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if len(peer.received()) >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return len(peer.received())
}

// TestRouteQueryDeadlineWakesPing: with the idle ping at 10 s and a peer that
// swallows every frame, a query that reaches its deadline makes the link ping
// at once and close within the deadline plus the ping timeout, long before the
// idle ping.
func TestRouteQueryDeadlineWakesPing(t *testing.T) {
	link, peer := newWakeLink(t, false)

	ctx, cancel := astral.NewContext(nil).WithIdentity(astral.Anyone).WithTimeout(wakeDeadline)
	defer cancel()
	start := time.Now()
	if err := routeOver(link, ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RouteQuery = %v; want the deadline", err)
	}

	if got := waitProbes(peer, 1, wakeSlack); got != 1 {
		t.Fatalf("peer received %v probes; want 1", got)
	}
	if at := peer.received()[0].Sub(start); at > wakeDeadline+wakeSlack {
		t.Fatalf("probe arrived %v after the query was sent; want about the deadline %v", at, wakeDeadline)
	}
	waitDone(t, link, time.Until(start.Add(wakeDeadline+wakeTimeout+wakeSlack)), "link whose peer swallows frames")
	if err := link.Err(); err == nil {
		t.Fatal("link closed with no error")
	}
}

// TestRouteQueryCancelDoesNotWake: a caller that cancels its query sends no
// ping, and the link stays open past the ping timeout.
func TestRouteQueryCancelDoesNotWake(t *testing.T) {
	link, peer := newWakeLink(t, false)

	ctx, cancel := astral.NewContext(nil).WithIdentity(astral.Anyone).WithCancel()
	defer cancel()
	time.AfterFunc(wakeDeadline/2, cancel)
	if err := routeOver(link, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RouteQuery = %v; want the cancellation", err)
	}

	time.Sleep(wakeTimeout + wakeSlack)
	if got := len(peer.received()); got != 0 {
		t.Fatalf("peer received %v probes after a cancelled query; want 0", got)
	}
	if err := link.Err(); err != nil {
		t.Fatalf("link closed after a cancelled query: %v", err)
	}
}

// TestSlowHealthyLinkStaysOpenAfterTimeout: a peer that answers pings but not
// the query gets one ping at the deadline, answers it, and the link stays open
// past the ping timeout.
func TestSlowHealthyLinkStaysOpenAfterTimeout(t *testing.T) {
	link, peer := newWakeLink(t, true)

	ctx, cancel := astral.NewContext(nil).WithIdentity(astral.Anyone).WithTimeout(wakeDeadline)
	defer cancel()
	if err := routeOver(link, ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RouteQuery = %v; want the deadline", err)
	}

	if got := waitProbes(peer, 1, wakeSlack); got != 1 {
		t.Fatalf("peer received %v probes; want 1", got)
	}
	select {
	case <-peer.pongs:
		t.Fatal("peer received a pong; it sends none")
	case <-link.Done():
		t.Fatalf("link closed although the peer answered its ping: %v", link.Err())
	case <-time.After(2 * wakeTimeout):
	}
	if err := link.Err(); err != nil {
		t.Fatalf("link closed although the peer answered its ping: %v", err)
	}
}

// TestTimeoutBurstCoalescesWake: a burst of timeouts on one link leaves at
// most one wake pending, and over a link whose peer swallows frames yields one
// ping and no overlapping one.
func TestTimeoutBurstCoalescesWake(t *testing.T) {
	t.Run("pending wake", func(t *testing.T) {
		// why a literal: no ping loop runs, so nothing consumes the wakes.
		link := &Link{wakeCh: make(chan struct{}, 1)}
		m := &Mux{onSilentTimeout: link.Wake}
		for i := 0; i < 50; i++ {
			m.onSilentTimeout()
		}
		if got := len(link.wakeCh); got != 1 {
			t.Fatalf("%v wakes pending after 50 timeouts; want 1", got)
		}
	})

	t.Run("one ping", func(t *testing.T) {
		link, peer := newWakeLink(t, false)

		const burst = 20
		errs := make(chan error, burst)
		for i := 0; i < burst; i++ {
			go func() {
				ctx, cancel := astral.NewContext(nil).WithIdentity(astral.Anyone).WithTimeout(wakeDeadline)
				defer cancel()
				errs <- routeOver(link, ctx)
			}()
		}
		for i := 0; i < burst; i++ {
			if err := <-errs; !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("RouteQuery = %v; want the deadline", err)
			}
		}

		waitDone(t, link, wakeTimeout+wakeSlack, "link whose peer swallows frames")
		if got := len(peer.received()); got != 1 {
			t.Fatalf("peer received %v probes for %v timeouts; want 1", got, burst)
		}
	})
}

// hookRuns routes one query from a mux over a pipe under a deadline and
// returns the error and how often the mux ran its timeout hook. The peer is
// a mux running router, or, when router is nil, one that reads and never answers.
func hookRuns(t *testing.T, router astral.Router, deadline time.Duration) (error, int32) {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)
	if router == nil {
		go io.Copy(io.Discard, b)
	} else {
		testMux(t, b, remoteID, localID).SetRouter(router)
	}
	var woken atomic.Int32
	local.onSilentTimeout = func() { woken.Add(1) }

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(deadline)
	defer cancel()
	_, err := local.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})
	return err, woken.Load()
}

// TestRouteQueryWakeOnlyOnSilentDeadline: the hook runs once for a query that
// reaches its deadline, and never for one the peer answers, even with a refusal.
func TestRouteQueryWakeOnlyOnSilentDeadline(t *testing.T) {
	err, woken := hookRuns(t, &failingRouter{err: astral.NewErrRouteNotFound()}, effectTimeout)
	assertRouteNotFound(t, err)
	if woken != 0 {
		t.Fatalf("hook ran %v times for an answered query; want 0", woken)
	}

	err, woken = hookRuns(t, nil, wakeDeadline)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RouteQuery = %v; want the deadline", err)
	}
	if woken != 1 {
		t.Fatalf("hook ran %v times for a query that timed out; want 1", woken)
	}
}
