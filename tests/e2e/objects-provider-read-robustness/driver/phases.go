package main

import (
	"bytes"
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// callTimeout bounds every call well past the bounds checked, so a call that
// overruns is measured rather than cut.
const callTimeout = 150 * time.Second

func timed(start time.Time, err error) call {
	c := call{Seconds: time.Since(start).Seconds(), Err: provider.ErrText(err)}
	var rejected *astral.ErrRejected
	if errors.As(err, &rejected) {
		c.Rejected, c.Code = true, int(rejected.Code)
	}
	return c
}

// hold waits up to limit for node1 to close G's read of id and returns how
// long G held it, or -1.
func hold(g *provider.Provider, id *astral.ObjectID, limit time.Duration) float64 {
	deadline := time.Now().Add(limit)
	for {
		if hs := g.Holds(provider.Read, id.String()); len(hs) > 0 {
			return hs[0].Seconds()
		}
		if time.Now().After(deadline) {
			return -1
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// phaseA runs find(O1), read(O1) and read(O2) with F claiming
// [G, G, node2, node2, node1, D, R] and H holding every find open.
func phaseA(w *world, f *facts) error {
	w.f.SetClaims(w.g.App.ID, w.g.App.ID, w.node2, w.node2, w.node1, w.d.ID, w.r)
	cli := objectscli.New(nil, w.user)

	start := time.Now()
	ids, err := provider.FindAll(w.ctx.IncludeZone(astral.ZoneNetwork), cli, w.o1, callTimeout)
	f.FindACall = timed(start, err)
	for _, id := range ids {
		f.FindA = append(f.FindA, id.String())
	}

	// why up to readO1Attempts reads: node1 takes the first candidate to
	// deliver a byte, and when it finds node2 itself before F's claims of G
	// arrive, node2 wins with G never dialed. That read says nothing about a
	// silent candidate, so the read repeats until one has G in play; node1
	// keeps no candidate between reads (op_read.go).
	for attempt := 1; attempt <= readO1Attempts && f.GReadO1 == 0; attempt++ {
		f.ReadO1Attempts = attempt
		start = time.Now()
		data, err := provider.ReadAll(w.ctx, cli, w.o1, callTimeout)
		f.ReadO1 = timed(start, err)
		f.ReadO1Bytes = len(data)
		f.ReadO1IsO1 = false
		if err == nil {
			id, _ := astral.Resolve(bytes.NewReader(data))
			f.ReadO1IsO1 = id != nil && id.IsEqual(w.o1)
		}
		f.GReadO1 = poll(func() int { return w.g.Count(provider.Read, w.o1.String()) }, 2*time.Second)
	}

	// why a wiring check: without F and H asked, and G dialed, nothing below
	// says anything about a bad candidate.
	f.FFindO1 = w.f.Count(provider.Find, w.o1.String())
	f.HFindO1 = w.h.Count(provider.Find, w.o1.String())
	if err := f.checks.Require("finders_asked", f.FFindO1 > 0 && f.HFindO1 > 0,
		"F asked %d times, H %d times for O1; want both asked", f.FFindO1, f.HFindO1); err != nil {
		return err
	}
	if err := f.checks.Require("silent_reader_dialed", f.GReadO1 > 0,
		"G was never dialed for O1 within 2 s of any of %d reads", f.ReadO1Attempts); err != nil {
		return err
	}
	if err := checkFindA(w, f); err != nil {
		return err
	}
	if err := f.checks.Check("read_o1_from_holder", f.ReadO1.Err == "" && f.ReadO1IsO1 && f.ReadO1.Seconds < readO1Bound.Seconds(),
		"read(O1) answered %d B (hash to O1: %v) in %.1f s, err %q; want O1's bytes under %v",
		f.ReadO1Bytes, f.ReadO1IsO1, f.ReadO1.Seconds, f.ReadO1.Err, readO1Bound); err != nil {
		return err
	}

	start = time.Now()
	_, err = provider.ReadAll(w.ctx, cli, w.o2, callTimeout)
	f.ReadO2A = timed(start, err)
	f.GReadO2 = w.g.Count(provider.Read, w.o2.String())
	if err := f.checks.Check("read_o2_rejected_past_hangs", f.ReadO2A.Rejected && f.ReadO2A.Seconds < readO2HangBound.Seconds(),
		"read(O2) with a hanging finder and a silent reader ended in %.1f s with %q; want a rejection under %v",
		f.ReadO2A.Seconds, f.ReadO2A.Err, readO2HangBound); err != nil {
		return err
	}
	if err := f.checks.Check("silent_reader_dialed_once", f.GReadO2 == 1,
		"G was dialed %d times for O2 although F claimed it twice; want once", f.GReadO2); err != nil {
		return err
	}
	// why: the read is answered before G's timer runs out, so only G sees
	// whether node1 ever ends the silent attempt.
	f.GHoldO2 = hold(w.g, w.o2, holdBound+5*time.Second)
	f.GHoldO1 = hold(w.g, w.o1, 0)
	return f.checks.Check("silent_reader_released", f.GHoldO2 >= 0 && f.GHoldO2 < holdBound.Seconds(),
		"node1 held G's read of O2 for %.1f s (-1: never closed); want it closed under %v", f.GHoldO2, holdBound)
}

// checkFindA checks find(O1): no identity twice, node2 once although three
// sources name it, F's unverified claims G and R relayed, within findBound.
func checkFindA(w *world, f *facts) error {
	seen := map[string]int{}
	for _, id := range f.FindA {
		seen[id]++
	}
	repeated := []string{}
	for id, n := range seen {
		if n > 1 {
			repeated = append(repeated, f.Names[id])
		}
	}
	ok := f.FindACall.Err == "" && len(repeated) == 0 && seen[w.node2.String()] == 1 &&
		seen[w.g.App.ID.String()] == 1 && seen[w.r.String()] == 1 && f.FindACall.Seconds < findBound.Seconds()
	return f.checks.Check("find_names_each_once", ok,
		"find(O1) named %v in %.1f s (err %q), repeated %v; want node2, G and R once each and no repeat under %v",
		f.FindA, f.FindACall.Seconds, f.FindACall.Err, repeated, findBound)
}

// phaseB reads O2 again with F claiming [node1, node2, D, R] and H answering
// at once: nothing holds the read open but the candidates themselves.
func phaseB(w *world, f *facts) error {
	w.f.SetClaims(w.node1, w.node2, w.d.ID, w.r)
	w.h.SetMode(provider.Find, provider.Answer)
	start := time.Now()
	_, err := provider.ReadAll(w.ctx, objectscli.New(nil, w.user), w.o2, callTimeout)
	f.ReadO2B = timed(start, err)
	f.GReadO2B = w.g.Count(provider.Read, w.o2.String())
	// why code 1: objects.read answers every failure with a plain rejection
	// (op_read.go); an internal error or a timeout is another failure.
	return f.checks.Check("read_o2_rejected_promptly",
		f.ReadO2B.Rejected && f.ReadO2B.Code == astral.CodeRejected && f.ReadO2B.Seconds < readO2Bound.Seconds(),
		"read(O2) with node1, node2, D and R as candidates ended in %.1f s with %q; want a plain rejection (code %d) under %v",
		f.ReadO2B.Seconds, f.ReadO2B.Err, astral.CodeRejected, readO2Bound)
}

// liar has L answer describe and search with node2's identity as SourceID:
// node1 must restamp both with L's own.
func liar(w *world, f *facts) error {
	nonce := astral.NewNonce().String()
	f.Label = "objects-provider-read-robustness label " + nonce
	f.Word = "objects-provider-read-robustness-" + nonce
	x, err := astral.ResolveObjectID(astral.NewString16("objects-provider-read-robustness X " + nonce))
	if err != nil {
		return err
	}
	f.X = x.String()
	w.l.SetDescriptors(&objects.Descriptor{SourceID: w.node2, ObjectID: w.o1, Data: astral.NewString16(f.Label)})
	w.l.SetResults(&objects.SearchResult{SourceID: w.node2, ObjectID: x})
	cli := objectscli.New(nil, w.user)

	ds, err := provider.DescribeAll(w.ctx, cli, w.o1, 30*time.Second)
	f.DescribeErr = provider.ErrText(err)
	for _, d := range ds {
		if s, ok := d.Data.(*astral.String16); ok && string(*s) == f.Label {
			f.DescribeSources = append(f.DescribeSources, d.SourceID.String())
		}
	}
	rs, err := provider.SearchAll(w.ctx, cli, f.Word, 30*time.Second)
	f.SearchErr = provider.ErrText(err)
	for _, r := range rs {
		if r.ObjectID.IsEqual(x) {
			f.SearchSources = append(f.SearchSources, r.SourceID.String())
		}
	}
	l := w.l.App.ID.String()
	if err := f.checks.Check("descriptor_restamped", len(f.DescribeSources) == 1 && f.DescribeSources[0] == l,
		"the descriptors carrying L's label came from %v; want L %v once", f.DescribeSources, l); err != nil {
		return err
	}
	return f.checks.Check("search_result_restamped", len(f.SearchSources) == 1 && f.SearchSources[0] == l,
		"the results for X came from %v; want L %v once", f.SearchSources, l)
}

// poll returns count's value once it is above zero, or after limit.
func poll(count func() int, limit time.Duration) int {
	deadline := time.Now().Add(limit)
	for {
		n := count()
		if n > 0 || time.Now().After(deadline) {
			return n
		}
		time.Sleep(100 * time.Millisecond)
	}
}
