package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

// failingRouter answers every query with err.
type failingRouter struct{ err error }

func (r *failingRouter) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return nil, r.err
}

// outcomeQuery is a query from localID to remoteID, the linked node.
func outcomeQuery(localID, remoteID *astral.Identity) *astral.InFlightQuery {
	return astral.Launch(&astral.Query{
		Nonce:       astral.NewNonce(),
		Caller:      localID,
		Target:      remoteID,
		QueryString: "test.op",
	})
}

// routeOverLink routes one query from a mux to a peer mux whose router answers
// with err, and returns what the caller's mux answered.
func routeOverLink(t *testing.T, err error) error {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)
	peer := testMux(t, b, remoteID, localID)
	peer.SetRouter(&failingRouter{err: err})

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(effectTimeout)
	defer cancel()

	_, got := local.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})
	return got
}

// A peer with no route answers "no route here", which the caller reads as a
// missing route and never as a rejection.
func TestRouteQueryPeerWithoutARouteAnswersRouteNotFound(t *testing.T) {
	err := routeOverLink(t, astral.NewErrRouteNotFound())

	assertRouteNotFound(t, err)
}

// A peer's refusal and its own codes reach the caller unchanged; a peer's own
// 255 reaches it as the generic refusal, never as a missing route.
func TestRouteQueryPeerCodesCrossTheLink(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want uint8
	}{
		{rejectedWith(astral.DefaultRejectCode), astral.DefaultRejectCode},
		{rejectedWith(9), 9},
		{rejectedWith(astral.CodeUnavailable), astral.CodeUnavailable},
		{rejectedWith(frames.CodeNoRoute), frames.CodeRejected},
		{context.DeadlineExceeded, astral.CodeTimeout},
		{errors.New("onward link failed"), astral.CodeUnavailable},
	} {
		t.Run(fmt.Sprint(tc.err), func(t *testing.T) {
			assertRejectedWith(t, routeOverLink(t, tc.err), tc.want)
		})
	}
}

// linkCode is the Response code for each kind of router error.
func TestLinkCode(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want uint8
	}{
		{routeNotFound(), frames.CodeNoRoute},
		{fmt.Errorf("wrapped: %w", routeNotFound()), frames.CodeNoRoute},
		{context.DeadlineExceeded, astral.CodeTimeout},
		{errors.New("plain"), astral.CodeUnavailable},
		{context.Canceled, astral.CodeUnavailable},
		{rejectedWith(9), 9},
		{rejectedWith(astral.DefaultRejectCode), astral.DefaultRejectCode},
		{rejectedWith(frames.CodeNoRoute), frames.CodeRejected},
	} {
		if got := linkCode(tc.err); got != tc.want {
			t.Errorf("linkCode(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// A peer that never answers leaves the query to its deadline, which the
// caller reads as the deadline and not as a missing route.
func TestRouteQuerySilentPeerAnswersTheDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go io.Copy(io.Discard, b) // the peer reads and never answers

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)

	const deadline = 200 * time.Millisecond
	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(deadline)
	defer cancel()

	start := time.Now()
	_, err := local.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})
	took := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RouteQuery err = %v, want context.DeadlineExceeded", err)
	}
	if took < deadline || took > deadline+time.Second {
		t.Fatalf("RouteQuery answered after %v, want at the %v deadline", took, deadline)
	}
}

// A caller that cancels reads its cancel.
func TestRouteQueryCanceledCallerAnswersCanceled(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go io.Copy(io.Discard, b)

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithCancel()
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := local.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RouteQuery err = %v, want context.Canceled", err)
	}
}

// A link that cannot write the query answers unavailable at once.
func TestRouteQueryClosedChannelAnswersUnavailable(t *testing.T) {
	a, b := net.Pipe()
	b.Close()
	a.Close()

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(effectTimeout)
	defer cancel()

	start := time.Now()
	_, err := local.RouteQuery(ctx, outcomeQuery(localID, remoteID), nopWriteCloser{})
	took := time.Since(start)

	assertRejectedWith(t, err, astral.CodeUnavailable)
	if took > 10*time.Millisecond {
		t.Fatalf("RouteQuery answered after %v, want within 10ms", took)
	}
}

// A link that goes down while a query waits for its Response answers
// unavailable at once. A Response arriving after the teardown finds no
// session; TestTeardownAndResponseSettleARoutingSessionOnce covers a Response
// that still finds it.
func TestRouteQueryLinkDownWhileRoutingAnswersUnavailableOnce(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go io.Copy(io.Discard, b)

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	local := testMux(t, a, localID, remoteID)

	ctx, cancel := astral.NewContext(nil).WithIdentity(localID).WithTimeout(effectTimeout)
	defer cancel()
	q := outcomeQuery(localID, remoteID)

	answered := make(chan error, 1)
	go func() {
		_, err := local.RouteQuery(ctx, q, nopWriteCloser{})
		answered <- err
	}()

	var s *session
	for deadline := time.Now().Add(effectTimeout); s == nil && time.Now().Before(deadline); {
		s, _ = local.sessions.Get(q.Nonce)
		time.Sleep(time.Millisecond)
	}
	if s == nil {
		t.Fatal("the query never opened a session")
	}

	start := time.Now()
	local.closeAllSessions()
	// the late Response finds the session settled and sends nothing
	local.handleResponse(&frames.Response{Nonce: q.Nonce, ErrCode: frames.CodeAccepted})

	select {
	case err := <-answered:
		assertRejectedWith(t, err, astral.CodeUnavailable)
		if took := time.Since(start); took > time.Second {
			t.Fatalf("RouteQuery answered %v after the teardown, want within 1s", took)
		}
	case <-time.After(effectTimeout):
		t.Fatal("RouteQuery did not answer after the link went down")
	}

	select {
	case code := <-s.routingResult:
		t.Fatalf("routingResult held a second result %v", code)
	default:
	}
}

// routingSession registers an outbound session that waits for its Response
// on a mux linked to remoteID.
func routingSession(t *testing.T) (*Mux, *session) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	go io.Copy(io.Discard, b)

	localID, remoteID := astral.GenerateIdentity(), astral.GenerateIdentity()
	m := testMux(t, a, localID, remoteID)
	s, ok := m.createSession(astral.NewNonce(), remoteID, nil, "test.op", true, 0)
	if !ok {
		t.Fatal("createSession failed")
	}
	return m, s
}

// drainResults answers every value s.routingResult receives within wait.
func drainResults(s *session, wait time.Duration) []uint8 {
	var got []uint8
	timeout := time.After(wait)
	for {
		select {
		case code := <-s.routingResult:
			got = append(got, code)
		case <-timeout:
			return got
		}
	}
}

// A Response that settled a routing session leaves the teardown nothing to
// send, so the caller never reads a second result for one query.
func TestTeardownAfterAResponseSendsNothingMore(t *testing.T) {
	for _, code := range []uint8{frames.CodeAccepted, astral.DefaultRejectCode, frames.CodeNoRoute} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			m, s := routingSession(t)

			m.handleResponse(&frames.Response{Nonce: s.Nonce, ErrCode: astral.Uint8(code)})
			if got := <-s.routingResult; got != code {
				t.Fatalf("routingResult = %v, want the Response code %v", got, code)
			}

			// the session is still registered: the teardown reaches it
			m.closeAllSessions()

			if got := drainResults(s, 50*time.Millisecond); len(got) != 0 {
				t.Fatalf("the teardown sent %v after the Response settled the session", got)
			}
		})
	}
}

// A Response and a teardown racing for one routing session settle it once:
// routingResult holds exactly one value, and neither side blocks.
func TestTeardownAndResponseSettleARoutingSessionOnce(t *testing.T) {
	for i := 0; i < 1000; i++ {
		m, s := routingSession(t)

		start := make(chan struct{})
		done := make(chan struct{}, 2)
		go func() {
			<-start
			m.handleResponse(&frames.Response{Nonce: s.Nonce, ErrCode: astral.Uint8(astral.DefaultRejectCode)})
			done <- struct{}{}
		}()
		go func() {
			<-start
			m.closeAllSessions()
			done <- struct{}{}
		}()
		close(start)

		// note: a second send blocks on the full buffer, so a double send
		// fails here as a blocked side.
		for range 2 {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatalf("round %d: a side blocked sending on routingResult", i)
			}
		}
		var got []uint8
		for len(s.routingResult) > 0 {
			got = append(got, <-s.routingResult)
		}

		if len(got) != 1 {
			t.Fatalf("round %d: routingResult received %v, want exactly one value", i, got)
		}
	}
}
