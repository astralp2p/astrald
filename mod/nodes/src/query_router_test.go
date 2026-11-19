package nodes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

// fakeRelays stands in for the relays of one query. A relay in unreached fails
// to be reached with the error it holds. Every other relay answers the query
// with what answers holds for it, or accepts, and every relay asked the query
// is recorded in order.
type fakeRelays struct {
	unreached map[*astral.Identity]error
	answers   map[*astral.Identity]error
	asked     []*astral.Identity
}

// fakeRelay is one reached relay of fakeRelays.
type fakeRelay struct {
	relays *fakeRelays
	id     *astral.Identity
}

// acceptingConn is the stream a relay that accepts answers.
type acceptingConn struct{ io.WriteCloser }

func (f *fakeRelays) reach(relayID *astral.Identity) (astral.Router, error) {
	if err := f.unreached[relayID]; err != nil {
		return nil, err
	}
	return &fakeRelay{relays: f, id: relayID}, nil
}

func (r *fakeRelay) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	r.relays.asked = append(r.relays.asked, r.id)
	if err := r.relays.answers[r.id]; err != nil {
		return nil, err
	}
	return &acceptingConn{}, nil
}

func (f *fakeRelays) route(relays []*astral.Identity) (io.WriteCloser, error) {
	return (&relayedQuery{ctx: astral.NewContext(nil), reach: f.reach}).route(relays)
}

func newRelays(n int) []*astral.Identity {
	relays := make([]*astral.Identity, n)
	for i := range relays {
		relays[i] = astral.GenerateIdentity()
	}
	return relays
}

func rejectedWith(code uint8) error {
	_, err := query.RejectWithCode(code)
	return err
}

func routeNotFound() error {
	_, err := query.RouteNotFound()
	return err
}

func assertAsked(t *testing.T, got, want []*astral.Identity) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("asked %v relays, want %v", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("relay %v asked out of order", i)
		}
	}
}

func assertRejectedWith(t *testing.T, err error, code uint8) {
	t.Helper()
	var rejected *astral.ErrRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("routing answered %v, want a rejection with code %v", err, code)
	}
	if rejected.Code != code {
		t.Fatalf("routing answered code %v, want %v", rejected.Code, code)
	}
}

func assertRouteNotFound(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, &astral.ErrRouteNotFound{}) {
		t.Fatalf("routing answered %v, want a missing route", err)
	}
	var rejected *astral.ErrRejected
	if errors.As(err, &rejected) {
		t.Fatalf("routing answered a rejection with code %v, want a missing route", rejected.Code)
	}
}

// A relay that accepts wins, even after another rejected, and no later relay
// is asked.
func TestAnAcceptingRelayWinsOverAnEarlierRejection(t *testing.T) {
	relays := newRelays(3)
	f := &fakeRelays{answers: map[*astral.Identity]error{relays[0]: rejectedWith(5)}}

	rw, err := f.route(relays)
	if err != nil {
		t.Fatalf("routing answered %v, want the second relay's accept", err)
	}
	if _, ok := rw.(*acceptingConn); !ok {
		t.Fatalf("routing answered %T, not the accepting relay's stream", rw)
	}
	assertAsked(t, f.asked, relays[:2])
}

// When no relay accepts, the first refusal answers with its own code, and
// every relay was asked in order before it did.
func TestTheFirstRefusalAnswersWhenNoRelayAccepts(t *testing.T) {
	relays := newRelays(4)
	f := &fakeRelays{answers: map[*astral.Identity]error{
		relays[0]: routeNotFound(),
		relays[1]: rejectedWith(5),
		relays[2]: errors.New("link closed"),
		relays[3]: rejectedWith(9),
	}}

	_, err := f.route(relays)

	assertRejectedWith(t, err, 5)
	assertAsked(t, f.asked, relays)
}

// A missing route is the answer only when every relay answered no route here.
func TestOnlyMissingRoutesAnswerAMissingRoute(t *testing.T) {
	relays := newRelays(2)
	f := &fakeRelays{answers: map[*astral.Identity]error{
		relays[0]: routeNotFound(),
		relays[1]: routeNotFound(),
	}}

	_, err := f.route(relays)

	assertRouteNotFound(t, err)
	assertAsked(t, f.asked, relays)

	_, err = f.route(nil)
	assertRouteNotFound(t, err)
}

// The generic code is a refusal: it outranks a missing route on either side
// of it, and the first refusal wins over a later one.
func TestTheGenericRejectionIsARefusal(t *testing.T) {
	relays := newRelays(3)
	f := &fakeRelays{answers: map[*astral.Identity]error{
		relays[0]: routeNotFound(),
		relays[1]: rejectedWith(astral.DefaultRejectCode),
		relays[2]: rejectedWith(5),
	}}

	_, err := f.route(relays[:2])
	assertRejectedWith(t, err, astral.DefaultRejectCode)

	f.asked = nil
	_, err = f.route(relays)
	assertRejectedWith(t, err, astral.DefaultRejectCode)
	assertAsked(t, f.asked, relays)
}

// A relay that is not reached, or that the caller's proof does not get
// through to, is never asked the query and answers unavailable: a later
// attempt may reach it.
func TestAnUnreachedRelayIsUnavailable(t *testing.T) {
	relays := newRelays(3)
	f := &fakeRelays{unreached: map[*astral.Identity]error{
		relays[0]: fmt.Errorf("push failed: %w", rejectedWith(5)),
		relays[1]: errors.New("link not produced"),
		relays[2]: rejectedWith(9),
	}}

	_, err := f.route(relays)
	assertRejectedWith(t, err, astral.CodeUnavailable)
	assertAsked(t, f.asked, nil)
}

// A relay that receives the caller's proof and refuses it refuses the caller:
// the answer is final, and it outranks an unreachable relay.
func TestARefusedProofIsARefusal(t *testing.T) {
	relays := newRelays(2)
	f := &fakeRelays{unreached: map[*astral.Identity]error{
		relays[0]: errors.New("link not produced"),
		relays[1]: fmt.Errorf("push failed: %w", objects.ErrPushRejected),
	}}

	_, err := f.route(relays)
	assertRejectedWith(t, err, astral.DefaultRejectCode)
	assertAsked(t, f.asked, nil)
}

// Unavailable outranks a missing route; a refusal outranks unavailable,
// whichever comes first.
func TestUnavailableRanksBetweenARefusalAndAMissingRoute(t *testing.T) {
	relays := newRelays(2)

	// [reach error, no route] -> 7
	f := &fakeRelays{
		unreached: map[*astral.Identity]error{relays[0]: errors.New("link not produced")},
		answers:   map[*astral.Identity]error{relays[1]: routeNotFound()},
	}
	_, err := f.route(relays)
	assertRejectedWith(t, err, astral.CodeUnavailable)

	// [no route, a relay answering 7] -> 7
	f = &fakeRelays{answers: map[*astral.Identity]error{
		relays[0]: routeNotFound(),
		relays[1]: rejectedWith(astral.CodeUnavailable),
	}}
	_, err = f.route(relays)
	assertRejectedWith(t, err, astral.CodeUnavailable)

	// [refused 1, reach error] -> 1
	f = &fakeRelays{
		answers:   map[*astral.Identity]error{relays[0]: rejectedWith(astral.DefaultRejectCode)},
		unreached: map[*astral.Identity]error{relays[1]: errors.New("link not produced")},
	}
	_, err = f.route(relays)
	assertRejectedWith(t, err, astral.DefaultRejectCode)

	// [reach error, refused 1] -> 1
	f = &fakeRelays{
		unreached: map[*astral.Identity]error{relays[0]: errors.New("link not produced")},
		answers:   map[*astral.Identity]error{relays[1]: rejectedWith(astral.DefaultRejectCode)},
	}
	_, err = f.route(relays)
	assertRejectedWith(t, err, astral.DefaultRejectCode)
}

// The target listed as its own relay is skipped as a missing route, not as an
// unavailable relay.
func TestTheTargetAsItsOwnRelayIsAMissingRoute(t *testing.T) {
	relays := newRelays(1)
	f := &fakeRelays{unreached: map[*astral.Identity]error{relays[0]: errTargetIsRelay}}

	_, err := f.route(relays)
	assertRouteNotFound(t, err)
}

// A routing context that ends stops the loop with its own error.
func TestAnEndedContextStopsTheRelayLoop(t *testing.T) {
	relays := newRelays(2)
	ctx, cancel := astral.NewContext(nil).WithCancel()
	f := &fakeRelays{answers: map[*astral.Identity]error{relays[0]: routeNotFound()}}
	cancel()

	_, err := (&relayedQuery{ctx: ctx, reach: f.reach}).route(relays)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("routing answered %v, want context.Canceled", err)
	}
	assertAsked(t, f.asked, relays[:1])
}

// A query whose routing context ended before any route answers that end, not
// a missing route or unavailable.
func TestUnrelayedAnswersTheEndedContext(t *testing.T) {
	canceled, cancel := astral.NewContext(nil).WithCancel()
	cancel()
	expired, cancelExpired := astral.NewContext(nil).WithTimeout(0)
	defer cancelExpired()

	for _, tc := range []struct {
		ctx  *astral.Context
		want error
	}{
		{canceled, context.Canceled},
		{expired, context.DeadlineExceeded},
	} {
		// note: a Module without a DB fails the test if unrelayed reaches it.
		_, err := (&Module{}).unrelayed(tc.ctx, astral.GenerateIdentity())
		if !errors.Is(err, tc.want) {
			t.Errorf("unrelayed err = %v, want %v", err, tc.want)
		}
	}
}
