package nodes

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/sig"
)

// seenModule returns a module with an endpoint database and a log.
func seenModule(t *testing.T) *Module {
	t.Helper()
	return &Module{db: testEndpointDB(t), log: log.New(astral.GenerateIdentity())}
}

// backdateClaim moves the claim of the endpoints of id back by d.
func backdateClaim(t *testing.T, db *DB, id *astral.Identity, d time.Duration) {
	t.Helper()
	var rows []*dbEndpointHealth
	if err := db.Where("identity = ?", id).Find(&rows).Error; err != nil {
		t.Fatalf("find health: %v", err)
	}
	for _, r := range rows {
		first, last := r.FirstSeen.Add(-d).UTC(), r.LastSeen.Add(-d).UTC()
		err := db.Model(&dbEndpointHealth{}).Where("identity = ? AND address = ?", id, r.Address).
			Updates(map[string]any{"first_seen": first, "last_seen": last}).Error
		if err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}
}

// TestRecordEndpointSeenCountsDays (criterion 1): replies on 3 different UTC
// days give SeenDays 3; many replies on one day give 1; FirstSeen keeps the
// first reply and LastSeen moves to the newest.
func TestRecordEndpointSeenCountsDays(t *testing.T) {
	db := testEndpointDB(t)
	id := astral.GenerateIdentity()
	key := endpointKey{"tcp", "192.168.1.20:1791"}
	today := time.Now().UTC()

	for range 5 {
		if err := db.RecordEndpointSeen(id, key, today); err != nil {
			t.Fatalf("RecordEndpointSeen: %v", err)
		}
	}
	if h := healthOf(t, db, id, key.address); h == nil || h.SeenDays != 1 {
		t.Fatalf("health after 5 replies on one day %+v; want SeenDays 1", h)
	}

	other := endpointKey{"tcp", "192.168.1.21:1791"}
	for _, ago := range []int{2, 2, 1, 1, 0, 0} {
		if err := db.RecordEndpointSeen(id, other, today.AddDate(0, 0, -ago)); err != nil {
			t.Fatalf("RecordEndpointSeen: %v", err)
		}
	}
	h := healthOf(t, db, id, other.address)
	if h == nil || h.SeenDays != 3 || h.FirstSeen == nil || h.LastSeen == nil {
		t.Fatalf("health after replies on 3 days %+v; want SeenDays 3 and both times set", h)
	}
	if !h.FirstSeen.Before(*h.LastSeen) || h.LastSeen.Before(today.Add(-time.Second)) {
		t.Fatalf("FirstSeen %v, LastSeen %v; want the first and the newest reply", h.FirstSeen, h.LastSeen)
	}
}

// TestStoreEndpointsCountsFirstHandDaysOnly (criterion 1): a first-hand reply
// counts the day of every endpoint it lists, once per day however many replies
// come; a second-hand reply counts none and creates no row.
func TestStoreEndpointsCountsFirstHandDaysOnly(t *testing.T) {
	mod := seenModule(t)
	id := astral.GenerateIdentity()
	const addr, ip = "192.168.1.20:1791", "192.168.1.20"

	for range 4 {
		if _, err := mod.storeEndpoints(id, endpointsWithTTL(ip), true); err != nil {
			t.Fatalf("storeEndpoints: %v", err)
		}
	}
	if h := healthOf(t, mod.db, id, addr); h == nil || h.SeenDays != 1 {
		t.Fatalf("health after 4 first-hand replies on one day %+v; want SeenDays 1", h)
	}

	for _, ago := range []time.Duration{48 * time.Hour, 48 * time.Hour} {
		backdateClaim(t, mod.db, id, ago/2)
		if _, err := mod.storeEndpoints(id, endpointsWithTTL(ip), true); err != nil {
			t.Fatalf("storeEndpoints: %v", err)
		}
	}
	if h := healthOf(t, mod.db, id, addr); h == nil || h.SeenDays != 3 {
		t.Fatalf("health after first-hand replies on 3 days %+v; want SeenDays 3", h)
	}

	before := healthOf(t, mod.db, id, addr)
	if _, err := mod.storeEndpoints(id, endpointsWithTTL(ip, "198.51.100.9"), false); err != nil {
		t.Fatalf("storeEndpoints: %v", err)
	}
	after := healthOf(t, mod.db, id, addr)
	if after.SeenDays != before.SeenDays || !after.LastSeen.Equal(*before.LastSeen) {
		t.Fatalf("second-hand reply changed the claim from %+v to %+v", before, after)
	}
	if h := healthOf(t, mod.db, id, "198.51.100.9:1791"); h != nil {
		t.Fatalf("second-hand reply created the health row %+v", h)
	}
}

// TestClaimSurvivesExpiryAndRecreation (criterion 2): the cleanup deletes an
// expired endpoint row, and the next first-hand reply that re-creates it
// finds FirstSeen and SeenDays kept and counts on from them.
func TestClaimSurvivesExpiryAndRecreation(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	db, id := f.mod.db, astral.GenerateIdentity()
	const addr, ip = "192.168.1.20:1791", "192.168.1.20"

	if _, err := f.mod.storeEndpoints(id, endpointsWithTTL(ip), true); err != nil {
		t.Fatalf("storeEndpoints: %v", err)
	}
	backdateClaim(t, db, id, 48*time.Hour)
	first := healthOf(t, db, id, addr)

	expired := time.Now().Add(-40 * 24 * time.Hour).UTC()
	if err := db.Model(&dbEndpoint{}).Where("identity = ?", id).Update("expires_at", expired).Error; err != nil {
		t.Fatalf("expire row: %v", err)
	}
	if err := f.mod.NewCleanupEndpointsTask().Run(f.mod.ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if rows, err := db.FindEndpoints(id); err != nil || len(rows) != 0 {
		t.Fatalf("endpoint rows after cleanup %v (%v); want the expired row deleted", rows, err)
	}

	if n, err := f.mod.storeEndpoints(id, endpointsWithTTL(ip), true); err != nil || n != 1 {
		t.Fatalf("storeEndpoints re-created %v rows (%v); want 1", n, err)
	}
	h := healthOf(t, db, id, addr)
	if h == nil || h.FirstSeen == nil || !h.FirstSeen.Equal(*first.FirstSeen) || h.SeenDays != 2 {
		t.Fatalf("claim after re-creation %+v; want FirstSeen %v kept and SeenDays 2", h, first.FirstSeen)
	}
}

// newScoreStrategy returns a health strategy whose node sits on network F1.
func newScoreStrategy(t *testing.T) (*BasicLinkStrategy, *dialLog, *fixedFingerprint) {
	t.Helper()
	var dials dialLog
	s := newHealthTestStrategy(t, dials.refuse)
	network := &fixedFingerprint{value: "F1"}
	s.mod.fingerprints = network
	return s, &dials, network
}

// plannedOrder returns the dial order planRows gives the rows addrs.
func plannedOrder(t *testing.T, s *BasicLinkStrategy, addrs ...string) (order []string) {
	t.Helper()
	ctx := astral.NewContext(context.Background())
	for row := range s.planRows(ctx, sig.ArrayToChan(tcpRows(t, addrs...))) {
		order = append(order, row.endpoint.Address())
		if row.done != nil {
			row.done()
		}
	}
	return
}

// dialsOf records dials of the tcp endpoint addr of the strategy's target: the
// outcomes in order, true for a success, each 10 ms long.
func dialsOf(t *testing.T, s *BasicLinkStrategy, addr string, outcomes ...bool) {
	t.Helper()
	for _, ok := range outcomes {
		var err error
		if !ok {
			err = context.DeadlineExceeded
		}
		s.mod.recordDial(s.target, mustTCP(t, addr), err, 10*time.Millisecond)
	}
}

// TestRecordDialKeepsRowPerNetwork (criterion 4): a private address dialed
// from F1 (success) and from F2 (failure) keeps two rows; the failures on F2
// leave F1's history and score as they were; a public address keeps one row
// whatever the network.
func TestRecordDialKeepsRowPerNetwork(t *testing.T) {
	s, _, network := newScoreStrategy(t)
	const private, public = "192.168.1.20:1791", "198.51.100.7:1791"

	dialsOf(t, s, private, true)
	dialsOf(t, s, public, true)
	onF1 := func() float64 {
		h, seen := s.mod.findEndpointHealth(s.target).of("tcp", private)
		return dialScore("tcp", h, seen)
	}
	scoreBefore := onF1()

	network.value = "F2"
	dialsOf(t, s, private, false, false, false)
	dialsOf(t, s, public, false)

	f1, f2 := healthAt(t, s.mod.db, s.target, private, "F1"), healthAt(t, s.mod.db, s.target, private, "F2")
	if f1 == nil || f2 == nil || f1.FailStreak != 0 || f1.LastOK == nil || f2.FailStreak != 3 || f2.LastOK != nil {
		t.Fatalf("private rows F1 %+v, F2 %+v; want two rows: F1 succeeded, F2 failed 3 times", f1, f2)
	}
	if global := healthAt(t, s.mod.db, s.target, private, ""); global != nil {
		t.Fatalf("private address has a global row %+v; want only the per-network rows", global)
	}
	network.value = "F1"
	if got := onF1(); got != scoreBefore {
		t.Fatalf("score on F1 is %v after failures on F2; want the unchanged %v", got, scoreBefore)
	}

	rows, err := s.mod.db.FindEndpointHealth(s.target, "", "F1", "F2")
	if err != nil {
		t.Fatalf("FindEndpointHealth: %v", err)
	}
	var publicRows []*dbEndpointHealth
	for _, r := range rows {
		if r.Address == public {
			publicRows = append(publicRows, r)
		}
	}
	if len(publicRows) != 1 || publicRows[0].Scope != "" || publicRows[0].LastOK == nil || publicRows[0].FailStreak != 1 {
		t.Fatalf("public rows %+v; want one global row holding the success from F1 and the failure from F2", publicRows)
	}
}

// TestPlanRowsOrdersByCurrentNetwork (criterion 4): the same private row that
// worked on F1 and failed on F2 dials first on F1 and last on F2.
func TestPlanRowsOrdersByCurrentNetwork(t *testing.T) {
	s, _, network := newScoreStrategy(t)
	const private, public = "192.168.1.20:1791", "198.51.100.7:1791"
	dialsOf(t, s, private, true, true, true)
	network.value = "F2"
	dialsOf(t, s, private, false, false, false)

	network.value = "F2"
	if got := plannedOrder(t, s, private, public); !slices.Equal(got, []string{public, private}) {
		t.Fatalf("order on F2 %v; want the failing %v last", got, private)
	}
	network.value = "F1"
	if got := plannedOrder(t, s, public, private); !slices.Equal(got, []string{private, public}) {
		t.Fatalf("order on F1 %v; want the usually working %v first", got, private)
	}
}

// TestPlanRowsPrefersUsualSuccessOverLastSuccess (criterion 5): an endpoint
// with 5 successes in 5 dials dials before one with 1 in 5 that succeeded most recently.
func TestPlanRowsPrefersUsualSuccessOverLastSuccess(t *testing.T) {
	s, _, _ := newScoreStrategy(t)
	const steady, lucky = "198.51.100.1:1791", "198.51.100.2:1791"
	dialsOf(t, s, steady, true, true, true, true, true)
	dialsOf(t, s, lucky, false, false, false, false, true)

	hs, hl := healthOf(t, s.mod.db, s.target, steady), healthOf(t, s.mod.db, s.target, lucky)
	if !hl.LastOK.After(*hs.LastOK) {
		t.Fatalf("lucky row LastOK %v is not after steady row LastOK %v; the test needs it to be", hl.LastOK, hs.LastOK)
	}
	if got := plannedOrder(t, s, lucky, steady); !slices.Equal(got, []string{steady, lucky}) {
		t.Fatalf("order %v; want %v first", got, steady)
	}
}

// TestEWMAFollowsDials: SuccessEWMA decays towards each outcome from the
// prior, LatencyEWMA follows the successes only and starts at the first one.
func TestEWMAFollowsDials(t *testing.T) {
	s, _, _ := newScoreStrategy(t)
	const addr = "198.51.100.1:1791"
	ref := ref(s.target, addr)
	at := time.Now()

	steps := []struct {
		ok      bool
		latency time.Duration
		wantLat time.Duration
	}{
		{false, 0, 0},
		{true, 100 * time.Millisecond, 100 * time.Millisecond},
		{false, 0, 100 * time.Millisecond},
		{true, 200 * time.Millisecond, 130 * time.Millisecond},
	}
	var outcomes []float64
	for _, st := range steps {
		var err error
		if st.ok {
			err = s.mod.db.RecordDialSuccess(ref, at, st.latency)
			outcomes = append(outcomes, 1)
		} else {
			err = s.mod.db.RecordDialFailure(ref, at)
			outcomes = append(outcomes, 0)
		}
		if err != nil {
			t.Fatalf("record dial: %v", err)
		}
		h := healthOf(t, s.mod.db, s.target, addr)
		if want := ewmaOf(successPrior, outcomes...); !approx(h.SuccessEWMA, want) || h.LatencyEWMA != st.wantLat {
			t.Fatalf("after %v dials %+v; want SuccessEWMA %v and LatencyEWMA %v", len(outcomes), h, want, st.wantLat)
		}
	}
}

// TestFailingRowRanksAfterRowWithoutHistory (criterion 6): a row with no
// history ranks above a row with FailStreak >= 3, however high the failing
// row's other terms are, and the failing row stays in the order.
func TestFailingRowRanksAfterRowWithoutHistory(t *testing.T) {
	now := time.Now()
	failing := &dialRow{
		endpoint:  tcpRows(t, "198.51.100.1:1791")[0],
		health:    &dbEndpointHealth{LastOK: &now, LastFail: &now, FailStreak: failingStreak, SuccessEWMA: 0.95},
		seenDays:  30,
		offSubnet: false,
	}
	fresh := &dialRow{endpoint: tcpRows(t, "198.51.100.2:1791")[0]}
	if failing.score() <= fresh.score() {
		t.Fatalf("failing row score %v does not exceed the fresh %v; the test needs it to", failing.score(), fresh.score())
	}

	rows := []*dialRow{failing, fresh}
	orderDialRows(rows, now)
	if rows[0] != fresh || rows[1] != failing {
		t.Fatalf("order %v, %v; want the row without history first and the failing row kept last",
			rows[0].endpoint.Address(), rows[1].endpoint.Address())
	}
}

// TestBasicDialsEveryRowWhenAllFail (criterion 6): rows with no history, a
// failure streak and a mixed record are all dialed in a round where every dial fails.
func TestBasicDialsEveryRowWhenAllFail(t *testing.T) {
	s, dials, _ := newScoreStrategy(t)
	addrs := []string{"198.51.100.1:1791", "198.51.100.2:1791", "198.51.100.3:1791", "192.168.1.20:1791"}
	dialsOf(t, s, addrs[0], false, false, false, false)
	dialsOf(t, s, addrs[1], true, false)
	dialsOf(t, s, addrs[3], false, false, false)
	s.mod.resolvers.Add(&stubResolver{rows: tcpRows(t, addrs...)})

	runRound(t, s)

	got := dials.dialed()
	slices.Sort(got)
	if want := slices.Sorted(slices.Values(addrs)); !slices.Equal(got, want) {
		t.Fatalf("dialed %v; want every row %v", got, want)
	}
}

// TestPlanRowsPrefersHabitualEndpoint (criterion 7): with equal dial history,
// a row its subject listed on 3 days dials before one listed once; with equal
// claims the order of the rows is kept.
func TestPlanRowsPrefersHabitualEndpoint(t *testing.T) {
	s, _, _ := newScoreStrategy(t)
	const habitual, oneOff = "198.51.100.1:1791", "198.51.100.2:1791"
	for _, addr := range []string{habitual, oneOff} {
		dialsOf(t, s, addr, true, false, true)
	}
	seen := func(addr string, days ...int) {
		for _, ago := range days {
			at := time.Now().UTC().AddDate(0, 0, -ago)
			if err := s.mod.db.RecordEndpointSeen(s.target, endpointKey{"tcp", addr}, at); err != nil {
				t.Fatalf("RecordEndpointSeen: %v", err)
			}
		}
	}
	seen(oneOff, 0)
	if got := plannedOrder(t, s, oneOff, habitual); !slices.Equal(got, []string{oneOff, habitual}) {
		t.Fatalf("order with equal claims %v; want the input order kept", got)
	}

	seen(habitual, 2, 1, 0)
	if got := plannedOrder(t, s, oneOff, habitual); !slices.Equal(got, []string{habitual, oneOff}) {
		t.Fatalf("order %v; want the row seen on 3 days (%v) first", got, habitual)
	}
}
