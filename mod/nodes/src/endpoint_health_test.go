package nodes

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	exonetapi "github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/mod/exonet"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

// subnetsIP is an ip module that reports fixed interface subnets.
type subnetsIP struct {
	ipmod.Module
	nets []*net.IPNet
}

func (s subnetsIP) LocalSubnets() ([]*net.IPNet, error) { return s.nets, nil }

// parsingExonet is a stubExonet that parses tcp rows, so the database resolver serves them.
type parsingExonet struct {
	stubExonet
}

func (e *parsingExonet) Parse(network, address string) (exonetapi.Endpoint, error) {
	if network != "tcp" {
		return nil, errors.ErrUnsupported
	}
	return tcp.ParseEndpoint(address)
}

// dialLog records the address of every dial in the order the dials start,
// and the budget each one ran under.
type dialLog struct {
	mu      sync.Mutex
	order   []string
	budgets map[string]time.Duration
}

// refuse records a dial and refuses it.
func (l *dialLog) refuse(ctx *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.order = append(l.order, ep.Address())
	if l.budgets == nil {
		l.budgets = map[string]time.Duration{}
	}
	if d, ok := ctx.Deadline(); ok {
		l.budgets[ep.Address()] = time.Until(d)
	}
	return nil, errors.New("refused")
}

func (l *dialLog) dialed() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.order)
}

func (l *dialLog) budget(addr string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d, ok := l.budgets[addr]
	return d, ok
}

// newHealthTestStrategy is newBasicTestStrategy with an endpoint database and a log.
func newHealthTestStrategy(t *testing.T, dial func(*astral.Context, exonetapi.Endpoint) (exonet.Conn, error)) *BasicLinkStrategy {
	t.Helper()
	s := newBasicTestStrategy(t, dial)
	s.mod.db = testEndpointDB(t)
	s.mod.log = log.New(s.target)
	return s
}

// runRound runs one basic round to its end.
func runRound(t *testing.T, s *BasicLinkStrategy) {
	t.Helper()
	done, release := s.Join(astral.NewContext(context.Background()))
	defer release()
	select {
	case <-done:
	case <-time.After(effectTimeout):
		t.Fatal("basic round did not finish")
	}
}

func tcpRows(t *testing.T, addrs ...string) (rows []*nodes.EndpointWithTTL) {
	t.Helper()
	for _, a := range addrs {
		rows = append(rows, nodes.NewEndpointWithTTL(mustTCP(t, a)))
	}
	return
}

// healthOf returns the global health row of target's tcp endpoint addr, or nil.
func healthOf(t *testing.T, db *DB, target *astral.Identity, addr string) *dbEndpointHealth {
	t.Helper()
	rows, err := db.FindEndpointHealth(target)
	if err != nil {
		t.Fatalf("FindEndpointHealth: %v", err)
	}
	for _, row := range rows {
		if row.Network == "tcp" && row.Address == addr {
			return row
		}
	}
	return nil
}

// TestBasicDialsOffSubnetRowLastWithShortBudget (criterion 4): a private row
// outside every own subnet dials after an on-subnet and a public row, under
// offSubnetRowBudget, and is still dialed; the on-subnet tcp row has no budget.
func TestBasicDialsOffSubnetRowLastWithShortBudget(t *testing.T) {
	var dials dialLog
	s := newHealthTestStrategy(t, dials.refuse)
	_, lan, _ := net.ParseCIDR("192.168.1.0/24")
	s.mod.IP = subnetsIP{nets: []*net.IPNet{lan}}
	s.mod.resolvers.Add(&stubResolver{rows: tcpRows(t, "10.20.0.5:1791", "192.168.1.20:1791", "198.51.100.7:1791")})

	runRound(t, s)

	got := dials.dialed()
	if len(got) != 3 || got[2] != "10.20.0.5:1791" {
		t.Fatalf("dial order %v; want the off-subnet row 10.20.0.5:1791 dialed last of 3", got)
	}
	if d, ok := dials.budget("10.20.0.5:1791"); !ok || d > offSubnetRowBudget || d <= 0 {
		t.Fatalf("off-subnet row budget %v (bounded %v); want within (0, %v]", d, ok, offSubnetRowBudget)
	}
	// note: the round budget bounds every row; an on-subnet tcp row has no budget of its own.
	if d, _ := dials.budget("192.168.1.20:1791"); d <= offSubnetRowBudget {
		t.Fatalf("on-subnet tcp row ran under a %v budget; want only the round budget", d)
	}
}

// TestBasicDialsFailingRowLast (criterion 5): a row that failed three times
// dials after every other row in the next round, and is still dialed.
func TestBasicDialsFailingRowLast(t *testing.T) {
	var dials dialLog
	s := newHealthTestStrategy(t, dials.refuse)
	resolver := &stubResolver{rows: tcpRows(t, "198.51.100.1:1791")}
	s.mod.resolvers.Add(resolver)

	for range failingStreak {
		runRound(t, s)
	}
	if h := healthOf(t, s.mod.db, s.target, "198.51.100.1:1791"); h == nil || h.FailStreak != failingStreak || h.LastFail == nil {
		t.Fatalf("health of the failing row %+v; want FailStreak %v and LastFail set", h, failingStreak)
	}

	resolver.rows = tcpRows(t, "198.51.100.1:1791", "198.51.100.2:1791", "198.51.100.3:1791")
	runRound(t, s)

	got := dials.dialed()[failingStreak:]
	if len(got) != 3 || got[2] != "198.51.100.1:1791" {
		t.Fatalf("dial order after %v failures %v; want 198.51.100.1:1791 dialed last of 3", failingStreak, got)
	}
}

// TestTryEndpointCancelledDialLeavesHealth (criterion 5): a dial whose round
// ends while it runs, as when another worker wins, records nothing.
func TestTryEndpointCancelledDialLeavesHealth(t *testing.T) {
	started := make(chan struct{})
	s := newHealthTestStrategy(t, func(ctx *astral.Context, _ exonetapi.Endpoint) (exonet.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	const addr = "198.51.100.1:1791"
	at := time.Now().UTC()
	for range 2 {
		if err := s.mod.db.RecordDialFailure(s.target, "tcp", addr, at); err != nil {
			t.Fatalf("seed failure: %v", err)
		}
	}

	round, cancel := astral.NewContext(context.Background()).WithCancel()
	done := make(chan *Link, 1)
	go func() { done <- s.tryEndpoint(round, &dialRow{endpoint: tcpRows(t, addr)[0]}) }()
	<-started
	cancel()
	if link := <-done; link != nil {
		t.Fatal("a cancelled dial returned a link")
	}

	if h := healthOf(t, s.mod.db, s.target, addr); h == nil || h.FailStreak != 2 {
		t.Fatalf("health after a cancelled dial %+v; want FailStreak 2 unchanged", h)
	}
}

// TestTryEndpointSuccessResetsFailStreak (criterion 5): a successful dial
// resets FailStreak and sets LastOK and LastLatency; a handshake that reaches
// another identity counts as a failure.
func TestTryEndpointSuccessResetsFailStreak(t *testing.T) {
	peer := newLinkEnd(t)
	dialer := newLinkEnd(t)
	dialer.mod.db = testEndpointDB(t)
	dialer.mod.Exonet = &stubExonet{dial: func(_ *astral.Context, ep exonetapi.Endpoint) (exonet.Conn, error) {
		return endpointConn{peer.pipeTo(t), ep}, nil
	}}
	const addr = "127.0.0.1:1791"
	for range failingStreak {
		if err := dialer.mod.db.RecordDialFailure(peer.id, "tcp", addr, time.Now().UTC()); err != nil {
			t.Fatalf("seed failure: %v", err)
		}
	}

	s := &BasicLinkStrategy{mod: dialer.mod, networks: basicNetworks, target: peer.id}
	ctx := astral.NewContext(context.Background())
	link := s.tryEndpoint(ctx, &dialRow{endpoint: tcpRows(t, addr)[0]})
	if link == nil {
		t.Fatal("tryEndpoint returned no link")
	}
	defer link.Close()

	h := healthOf(t, dialer.mod.db, peer.id, addr)
	if h == nil || h.FailStreak != 0 || h.LastOK == nil || h.LastLatency <= 0 || h.OKCount != 1 {
		t.Fatalf("health after a success %+v; want FailStreak 0, LastOK, LastLatency and OKCount 1 set", h)
	}

	// note: the row budget ends a handshake the peer never answers; the round context stays live.
	other := newTestIdentity(t)
	wrong := &BasicLinkStrategy{mod: dialer.mod, networks: basicNetworks, rowBudgets: map[string]time.Duration{"tcp": 2 * time.Second}, target: other}
	if l := wrong.tryEndpoint(ctx, &dialRow{endpoint: tcpRows(t, addr)[0]}); l != nil {
		l.Close()
		t.Fatal("a handshake with another identity returned a link")
	}
	if h := healthOf(t, dialer.mod.db, other, addr); h == nil || h.FailStreak != 1 {
		t.Fatalf("health after reaching another identity %+v; want FailStreak 1", h)
	}
}

// TestBasicFailingRoundsKeepEveryRow (criterion 6): every row fails for 10
// rounds; the endpoints table still holds every row and each is still dialed.
func TestBasicFailingRoundsKeepEveryRow(t *testing.T) {
	var dials dialLog
	s := newHealthTestStrategy(t, dials.refuse)
	s.mod.Exonet = &parsingExonet{stubExonet{dial: dials.refuse}}
	s.mod.resolvers.Add(&DBEndpointResolver{mod: s.mod})
	addrs := []string{"198.51.100.1:1791", "198.51.100.2:1791", "192.168.1.20:1791"}
	expiry := time.Now().Add(time.Hour).UTC()
	for _, a := range addrs {
		if err := s.mod.db.AddEndpoint(s.target, "tcp", a, &expiry); err != nil {
			t.Fatalf("seed row: %v", err)
		}
	}

	const rounds = 10
	for range rounds {
		runRound(t, s)
	}

	rows, err := s.mod.db.FindEndpoints(s.target)
	if err != nil || len(rows) != len(addrs) {
		t.Fatalf("after %v failing rounds the table holds %v rows (%v); want %v", rounds, len(rows), err, len(addrs))
	}
	if n := len(dials.dialed()); n != rounds*len(addrs) {
		t.Fatalf("%v dials in %v rounds; want every row in every round (%v)", n, rounds, rounds*len(addrs))
	}
	for _, a := range addrs {
		if h := healthOf(t, s.mod.db, s.target, a); h == nil || h.FailStreak != rounds {
			t.Fatalf("health of %v %+v; want FailStreak %v", a, h, rounds)
		}
	}
}

// TestCleanupKeepsHealthOfExpiredRowAndDropsStaleHealth (criterion 7): the
// cleanup deletes an expired endpoint row and keeps its health row; it deletes
// a health row last updated more than 30 days ago.
func TestCleanupKeepsHealthOfExpiredRowAndDropsStaleHealth(t *testing.T) {
	f := newRefreshFixture(t, defaultSiblingRefreshInterval)
	id := astral.GenerateIdentity()
	db := f.mod.db

	longExpired := time.Now().Add(-40 * 24 * time.Hour).UTC()
	if err := db.AddEndpoint(id, "tcp", "198.51.100.1:1791", &longExpired); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := db.RecordDialSuccess(id, "tcp", "198.51.100.1:1791", time.Now().UTC(), time.Millisecond); err != nil {
		t.Fatalf("seed health: %v", err)
	}
	stale := time.Now().Add(-endpointHealthRetention - time.Hour).UTC()
	if err := db.RecordDialFailure(id, "tcp", "198.51.100.2:1791", stale); err != nil {
		t.Fatalf("seed stale health: %v", err)
	}

	if err := f.mod.NewCleanupEndpointsTask().Run(f.mod.ctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	var left []*dbEndpoint
	if err := db.Where("identity = ?", id).Find(&left).Error; err != nil || len(left) != 0 {
		t.Fatalf("endpoint rows after cleanup %v (%v); want the expired row deleted", left, err)
	}
	if h := healthOf(t, db, id, "198.51.100.1:1791"); h == nil {
		t.Fatal("the health row of the expired endpoint was deleted; want it kept")
	}
	if h := healthOf(t, db, id, "198.51.100.2:1791"); h != nil {
		t.Fatalf("stale health row %+v survived cleanup; want it deleted", h)
	}
}

// TestOrderDialRows: rows with a recent success come first, newest first, then
// rows without a verdict, then failing and off-subnet rows; a recent success
// lifts an off-subnet row; equal rows keep their order and none is dropped.
func TestOrderDialRows(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { at := now.Add(-d); return &at }
	row := func(name string, h *dbEndpointHealth, offSubnet bool) *dialRow {
		return &dialRow{endpoint: tcpRows(t, name)[0], health: h, offSubnet: offSubnet}
	}

	rows := []*dialRow{
		row("10.0.0.1:1", nil, true),
		row("10.0.0.2:1", &dbEndpointHealth{FailStreak: failingStreak}, false),
		row("10.0.0.3:1", nil, false),
		row("10.0.0.4:1", &dbEndpointHealth{LastOK: ago(time.Hour)}, false),
		row("10.0.0.5:1", &dbEndpointHealth{LastOK: ago(time.Minute)}, true),
		row("10.0.0.6:1", &dbEndpointHealth{LastOK: ago(48 * time.Hour), FailStreak: 1}, false),
		row("10.0.0.7:1", nil, false),
	}
	orderDialRows(rows, now)

	var got []string
	for _, r := range rows {
		got = append(got, r.endpoint.Address())
	}
	want := []string{"10.0.0.5:1", "10.0.0.4:1", "10.0.0.3:1", "10.0.0.6:1", "10.0.0.7:1", "10.0.0.1:1", "10.0.0.2:1"}
	if !slices.Equal(got, want) {
		t.Fatalf("dial order %v; want %v", got, want)
	}
}
