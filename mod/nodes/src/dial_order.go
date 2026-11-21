package nodes

import (
	"cmp"
	"context"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

const (
	// recentDialOK is how long a successful dial puts a row first.
	recentDialOK = 24 * time.Hour
	// failingStreak is the failure streak from which a row is dialed last.
	failingStreak = 3
	// offSubnetRowBudget bounds the dial of a non-public row outside every own subnet.
	// why: such a row is usually a network the peer or this node left; a VPN
	// or a routed private network still answers within it.
	offSubnetRowBudget = time.Second
	// rowCollectWindow is the longest wait for the resolvers before the rows are ordered.
	// note: a row that arrives later is dialed after the ordered rows.
	rowCollectWindow = 150 * time.Millisecond
	// lastTierHeadStart is the longest the other rows dial before a last-tier row starts.
	// why: a last-tier row starts at once when every earlier row has finished,
	// so a round of dead rows does not wait.
	lastTierHeadStart = 500 * time.Millisecond
)

// dialTier is the coarse dial order class of a row; a lower tier dials first.
type dialTier int

const (
	tierNormal dialTier = iota // ordered by score
	tierLast                   // a failure streak, or an off-subnet local row
)

// dialRow is one resolved row of a dial round with what this node knows of it.
type dialRow struct {
	endpoint *nodes.EndpointWithTTL
	// health is the dial history of the row from the current network; nil when
	// this node never dialed the row from there.
	health *dbEndpointHealth
	// seenDays is how many days the subject listed the row; 0 for none.
	seenDays uint32
	// offSubnet is set for a non-public tcp or kcp row outside every own subnet.
	offSubnet bool
	// done is called once the row's dial returns; nil when nothing waits for it.
	done func()
}

// failing reports whether the row failed its last failingStreak dials.
func (r *dialRow) failing() bool {
	return r.health != nil && r.health.FailStreak >= failingStreak
}

// recentOK reports whether the row's dial succeeded within recentDialOK of now.
func (r *dialRow) recentOK(now time.Time) bool {
	return r.health != nil && r.health.LastOK != nil && now.Sub(*r.health.LastOK) < recentDialOK
}

// tier returns the dial tier of the row at now.
// why a recent success outranks the address class: health is evidence, the
// address class a guess, so a working VPN row is not dialed last.
func (r *dialRow) tier(now time.Time) dialTier {
	switch {
	case r.failing():
		return tierLast
	case r.recentOK(now):
		return tierNormal
	case r.offSubnet:
		return tierLast
	}
	return tierNormal
}

// score returns the dial score of the row; see dialScore.
func (r *dialRow) score() float64 {
	return dialScore(r.endpoint.Network(), r.health, r.seenDays)
}

// shortBudget reports whether the row dials under offSubnetRowBudget at now.
func (r *dialRow) shortBudget(now time.Time) bool {
	return r.offSubnet && (r.failing() || !r.recentOK(now))
}

// orderDialRows sorts rows into dial order at now: rows of the normal tier by
// descending score, then failing and off-subnet rows by descending score.
// The order of rows that compare equal is kept. No row is dropped.
// why: the score weighs the whole history of a row, so one lucky or unlucky dial does not reorder rows.
func orderDialRows(rows []*dialRow, now time.Time) {
	slices.SortStableFunc(rows, func(a, b *dialRow) int {
		if ta, tb := a.tier(now), b.tier(now); ta != tb {
			return cmp.Compare(ta, tb)
		}
		return cmp.Compare(b.score(), a.score())
	})
}

// rowAnnotator turns resolved rows of one round into dial rows, from one
// health query and one read of this node's subnets.
type rowAnnotator struct {
	health  *healthSet
	subnets []*net.IPNet
	// known is false when this node's subnets are unknown; no row is then off-subnet.
	known bool
}

func (s *BasicLinkStrategy) newRowAnnotator() *rowAnnotator {
	subnets, known := s.mod.ownSubnets()
	return &rowAnnotator{health: s.mod.findEndpointHealth(s.target), subnets: subnets, known: known}
}

func (a *rowAnnotator) annotate(e *nodes.EndpointWithTTL) *dialRow {
	health, seenDays := a.health.of(e.Network(), e.Address())
	return &dialRow{
		endpoint:  e,
		health:    health,
		seenDays:  seenDays,
		offSubnet: a.known && isOffSubnet(e.Network(), e.Address(), a.subnets),
	}
}

// planRows reads the round's rows from in and returns them in dial order.
// It waits for in up to rowCollectWindow, orders what arrived, holds last-tier
// rows back for up to lastTierHeadStart, then passes on later rows as they come.
func (s *BasicLinkStrategy) planRows(ctx *astral.Context, in <-chan *nodes.EndpointWithTTL) <-chan *dialRow {
	out := make(chan *dialRow)
	go func() {
		defer close(out)

		resolved, open := collectRows(ctx, in, rowCollectWindow)
		annotator := s.newRowAnnotator()
		rows := make([]*dialRow, 0, len(resolved))
		for _, e := range resolved {
			rows = append(rows, annotator.annotate(e))
		}

		now := time.Now()
		orderDialRows(rows, now)
		if !emitRows(ctx, rows, now, out) || !open {
			return
		}
		for {
			e, ok := recvRow(ctx, in)
			if !ok || !sendRow(ctx, out, annotator.annotate(e)) {
				return
			}
		}
	}()
	return out
}

// collectRows reads rows from in until in closes, window passes or ctx ends.
// open reports whether in may still yield rows.
func collectRows(ctx *astral.Context, in <-chan *nodes.EndpointWithTTL, window time.Duration) (rows []*nodes.EndpointWithTTL, open bool) {
	timer := time.NewTimer(window)
	defer timer.Stop()

	for {
		select {
		case e, ok := <-in:
			if !ok {
				return rows, false
			}
			rows = append(rows, e)
		case <-timer.C:
			return rows, true
		case <-ctx.Done():
			return rows, false
		}
	}
}

// emitRows sends ordered rows to out. Last-tier rows wait until every earlier
// row's dial returned or lastTierHeadStart passed. It reports false once ctx ended.
func emitRows(ctx *astral.Context, rows []*dialRow, now time.Time, out chan<- *dialRow) bool {
	var wg sync.WaitGroup
	i := 0
	for ; i < len(rows) && rows[i].tier(now) != tierLast; i++ {
		wg.Add(1)
		rows[i].done = wg.Done
		if !sendRow(ctx, out, rows[i]) {
			wg.Done()
			return false
		}
	}

	if i > 0 && i < len(rows) {
		waitHeadStart(ctx, &wg, lastTierHeadStart)
	}

	for ; i < len(rows); i++ {
		if !sendRow(ctx, out, rows[i]) {
			return false
		}
	}
	return true
}

// waitHeadStart returns once wg is done, d passed or ctx ended.
func waitHeadStart(ctx context.Context, wg *sync.WaitGroup, d time.Duration) {
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-finished:
	case <-timer.C:
	case <-ctx.Done():
	}
}

func sendRow(ctx context.Context, out chan<- *dialRow, row *dialRow) bool {
	select {
	case out <- row:
		return true
	case <-ctx.Done():
		return false
	}
}

func recvRow(ctx context.Context, in <-chan *nodes.EndpointWithTTL) (*nodes.EndpointWithTTL, bool) {
	select {
	case e, ok := <-in:
		return e, ok
	case <-ctx.Done():
		return nil, false
	}
}
