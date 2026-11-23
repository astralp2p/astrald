package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

const (
	// queryEvery is the time between two queries of a phase.
	queryEvery = 500 * time.Millisecond
	// queryDeadline outlasts the 60 s routing deadline, so a query that
	// waits for it shows as a timeout (astral-docs core-definitions/query.md).
	queryDeadline = 65 * time.Second
	// loopMin is how long a phase queries at least, from its black-hole.
	loopMin = 15 * time.Second
	// afterClose is how long a phase queries on after the dark link closed.
	afterClose = 5 * time.Second
	// loopMax bounds a phase whose dark link never closes.
	loopMax = 40 * time.Second
	// pollEvery is the link poll interval.
	pollEvery = 250 * time.Millisecond
)

// phase is one dark link and the queries node1 sent while it went dark.
// Times are wall-clock unix seconds of the host clock.
type phase struct {
	RelayLink   string   `json:"relay_link"`
	BlackholeAt float64  `json:"blackhole_at"`
	LastFrameAt float64  `json:"last_frame_at"`
	ClosedAt    *float64 `json:"closed_at"`
	ClosedAt2   *float64 `json:"closed_at_node2"`
	Forwarded   uint64   `json:"forwarded_bytes"`
	Queries     []record `json:"queries"`
}

// record is one query of node1 to node2.
type record struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Outcome string  `json:"outcome"`
	Code    int     `json:"code"`
	Err     string  `json:"err,omitempty"`
}

// darken black-holes relay, whose link is id, queries node2 from node1 until
// the link closed on node1 plus afterClose (at least loopMin, at most
// loopMax), and waits for every query to end.
func darken(ctx *astral.Context, p pair, r *relay, id astral.Nonce) (*phase, error) {
	r.blackhole()
	ph := &phase{RelayLink: id.String(), BlackholeAt: unix(time.Now()), LastFrameAt: unix(r.last()),
		Forwarded: r.forwarded.Load()}
	w := &linkWatch{}
	wctx, stop := ctx.WithCancel()
	defer stop()
	go w.run(wctx, p, id)

	var mu sync.Mutex
	var wg sync.WaitGroup
	start := time.Now()
	for !w.queried(start) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := ask(ctx, p)
			mu.Lock()
			defer mu.Unlock()
			ph.Queries = append(ph.Queries, rec)
		}()
		time.Sleep(queryEvery)
	}
	wg.Wait()
	stop()
	ph.ClosedAt, ph.ClosedAt2 = w.times()
	slices.SortFunc(ph.Queries, func(a, b record) int { return compare(a.Start, b.Start) })
	if ph.ClosedAt == nil {
		return ph, fmt.Errorf("node1 still listed the dark link %v %v after its black-hole", id, loopMax)
	}
	return ph, nil
}

func compare(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// linkWatch polls both nodes until neither lists a link, and records when
// each stopped listing it.
type linkWatch struct {
	mu           sync.Mutex
	gone1, gone2 *float64
}

func (w *linkWatch) run(ctx *astral.Context, p pair, id astral.Nonce) {
	for ctx.Err() == nil {
		w.check(ctx, p, id)
		w.mu.Lock()
		done := w.gone1 != nil && w.gone2 != nil
		w.mu.Unlock()
		if done {
			return
		}
		time.Sleep(pollEvery)
	}
}

// check records the first poll at which a node no longer lists the link.
func (w *linkWatch) check(ctx *astral.Context, p pair, id astral.Nonce) {
	ids1, err1 := linkIDs(ctx, p.admin1, p.n2.Identity)
	ids2, err2 := linkIDs(ctx, p.admin2, p.n1.Identity)
	now := unix(time.Now())
	w.mu.Lock()
	defer w.mu.Unlock()
	if err1 == nil && w.gone1 == nil && !slices.Contains(ids1, id) {
		w.gone1 = &now
	}
	if err2 == nil && w.gone2 == nil && !slices.Contains(ids2, id) {
		w.gone2 = &now
	}
}

func (w *linkWatch) times() (*float64, *float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gone1, w.gone2
}

// queried reports whether a phase begun at start has queried long enough.
func (w *linkWatch) queried(start time.Time) bool {
	gone, _ := w.times()
	since := time.Since(start)
	switch {
	case since >= loopMax:
		return true
	case since < loopMin || gone == nil:
		return false
	}
	return unix(time.Now()) >= *gone+afterClose.Seconds()
}

// ask queries node2's user.info as node1's admin and records the outcome.
func ask(ctx *astral.Context, p pair) record {
	start := time.Now()
	qctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(queryDeadline)
	defer cancel()
	ch, err := p.admin1.WithTarget(p.id2).QueryChannel(qctx, "user.info", nil)
	if err == nil {
		err = ch.Switch(channel.PassErrors, func(astral.Object) error { return channel.ErrBreak },
			channel.WithContext(qctx), channel.FailOnEOF())
		ch.Close()
	}
	end := time.Now()
	rec := record{Start: unix(start), End: unix(end)}
	rec.Outcome, rec.Code = classify(err)
	if err != nil {
		rec.Err = err.Error()
	}
	return rec
}

// classify maps a query's error to the routing outcome classes of astral-docs
// core-definitions/query.md, Routing outcomes.
func classify(err error) (string, int) {
	var rejected *astral.ErrRejected
	var msg *apphostapi.ErrorMsg
	switch {
	case err == nil:
		return "ok", 0
	case errors.As(err, &rejected) && rejected.Code == astral.CodeUnavailable:
		return "unavailable", int(rejected.Code)
	case errors.As(err, &rejected) && rejected.Code == astral.CodeTimeout:
		return "timeout", int(rejected.Code)
	case errors.As(err, &rejected):
		return fmt.Sprintf("rejected:%d", rejected.Code), int(rejected.Code)
	case errors.As(err, &msg) && string(msg.Code) == apphostapi.ErrCodeRouteNotFound,
		errors.Is(err, astral.NewErrRouteNotFound()):
		return "route_not_found", 0
	case errors.As(err, &msg) && string(msg.Code) == apphostapi.ErrCodeTimeout,
		errors.Is(err, astral.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout", 0
	}
	return "error", 0
}

func unix(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
