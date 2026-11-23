package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Kinds of a peer the app calls: a reported provider or a -targets identity
// is an app, a -probe-nodes identity is a probe.
const (
	kindApp   = "app"
	kindProbe = "probe"
)

// callRecord is one finished call. Times are wall-clock unix seconds of the
// host clock.
type callRecord struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Outcome string  `json:"outcome"`
	Code    int     `json:"code"`
	Ms      float64 `json:"ms"`
	Err     string  `json:"err,omitempty"`
	Reply   string  `json:"reply,omitempty"`
}

// removal is one retraction of a provider's offering from the follow.
type removal struct {
	T     float64 `json:"t"`
	Cause string  `json:"cause"`
}

// peerState is what the app knows of one provider or probed node.
type peerState struct {
	ReportedAt    float64        `json:"reported_at"`
	Target        bool           `json:"target"`
	Listed        bool           `json:"listed"`
	Removed       []removal      `json:"removed"`
	Restored      []float64      `json:"restored"`
	First         *callRecord    `json:"first"`
	Calls         int            `json:"calls"`
	Ok            int            `json:"ok"`
	ByOutcome     map[string]int `json:"by_outcome"`
	LastOk        *callRecord    `json:"last_ok"`
	LastFail      *callRecord    `json:"last_fail"`
	InflightSince *float64       `json:"inflight_since"`
	MaxMs         float64        `json:"max_ms"`
}

func newPeerState(t float64, target bool) *peerState {
	return &peerState{ReportedAt: t, Target: target, Listed: true,
		Removed: []removal{}, Restored: []float64{}, ByOutcome: map[string]int{}}
}

// initialRecord is the outcome of one follow boundary.
type initialRecord struct {
	T          float64  `json:"t"`
	Complete   bool     `json:"complete"`
	Incomplete []string `json:"incomplete"`
}

// appState is the whole state file.
type appState struct {
	Pid          int                   `json:"pid"`
	App          string                `json:"app"`
	Node         string                `json:"node"`
	Service      string                `json:"service"`
	StartedAt    float64               `json:"started_at"`
	RegisteredAt *float64              `json:"registered_at"`
	AdvertisedAt *float64              `json:"advertised_at"`
	Attaches     int                   `json:"attaches"`
	Disconnects  []string              `json:"disconnects"`
	FollowOpens  int                   `json:"follow_opens"`
	Initial      *initialRecord        `json:"initial"`
	FollowErr    string                `json:"follow_err"`
	NotGranted   []string              `json:"not_granted"`
	Fatal        string                `json:"fatal"`
	Served       map[string]int        `json:"served"`
	Providers    map[string]*peerState `json:"providers"`
	Probes       map[string]*peerState `json:"probes"`
}

// recorder holds the app's state and tells the writer of every change.
type recorder struct {
	mu    sync.Mutex
	st    appState
	dirty chan struct{}
}

func newRecorder(st appState) *recorder {
	st.Disconnects, st.NotGranted = []string{}, []string{}
	st.Served = map[string]int{}
	st.Providers, st.Probes = map[string]*peerState{}, map[string]*peerState{}
	return &recorder{st: st, dirty: make(chan struct{}, 1)}
}

// update applies f under the lock and marks the state dirty.
func (r *recorder) update(f func(st *appState)) {
	r.apply(f)
	select {
	case r.dirty <- struct{}{}:
	default:
	}
}

func (r *recorder) apply(f func(st *appState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f(&r.st)
}

// marshal is the state as JSON, read under the lock.
func (r *recorder) marshal() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return json.Marshal(&r.st)
}

func (r *recorder) peers(st *appState, kind string) map[string]*peerState {
	if kind == kindProbe {
		return st.Probes
	}
	return st.Providers
}

// report records a provider the follow listed, or a target or probed node
// the flags named. It answers true for a peer first seen, which needs a
// caller.
func (r *recorder) report(kind, id string, t float64, target bool) (first bool) {
	r.update(func(st *appState) {
		peers := r.peers(st, kind)
		p, ok := peers[id]
		switch {
		case !ok:
			peers[id] = newPeerState(t, target)
			first = true
		case !p.Listed:
			p.Listed = true
			p.Restored = append(p.Restored, t)
		}
	})
	return first
}

// remove records that the follow retracted a provider's offering.
func (r *recorder) remove(id string, t float64, cause string) {
	r.update(func(st *appState) {
		p, ok := st.Providers[id]
		if !ok || !p.Listed {
			return
		}
		p.Listed = false
		p.Removed = append(p.Removed, removal{T: t, Cause: cause})
	})
}

// callable reports whether the app calls the peer now: while it is listed,
// or while it is known with keepRemoved.
func (r *recorder) callable(kind, id string, keepRemoved bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers(&r.st, kind)[id]
	return ok && (p.Listed || keepRemoved)
}

// started records a call in flight to the peer.
func (r *recorder) started(kind, id string, t float64) {
	r.update(func(st *appState) {
		if p, ok := r.peers(st, kind)[id]; ok {
			p.InflightSince = &t
		}
	})
}

// finished folds one finished call into the peer's aggregate.
func (r *recorder) finished(kind, id string, c callRecord) {
	r.update(func(st *appState) {
		p, ok := r.peers(st, kind)[id]
		if !ok {
			return
		}
		p.InflightSince = nil
		p.Calls++
		p.ByOutcome[c.Outcome]++
		p.MaxMs = max(p.MaxMs, c.Ms)
		if p.First == nil {
			p.First = &c
		}
		if c.Outcome == outcomeOK {
			p.Ok++
			p.LastOk = &c
		} else {
			p.LastFail = &c
		}
	})
}

// stateWriter rewrites the state file by rename, at most once per every.
type stateWriter struct {
	path  string
	every time.Duration
	rec   *recorder
}

// run writes on every change until stop closes, then writes once more.
func (w *stateWriter) run(stop <-chan struct{}) {
	for {
		select {
		case <-w.rec.dirty:
		case <-stop:
			w.write()
			return
		}
		w.write()
		t := time.NewTimer(w.every)
		select {
		case <-t.C:
		case <-stop:
			t.Stop()
			w.write()
			return
		}
	}
}

// write replaces the state file whole.
// why rename: the harness polls the file and must never read half of it.
func (w *stateWriter) write() {
	raw, err := w.rec.marshal()
	if err != nil {
		fmt.Fprintln(os.Stderr, "meshapp: state:", err)
		return
	}
	tmp := w.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "meshapp: state:", err)
		return
	}
	if err := os.Rename(tmp, w.path); err != nil {
		fmt.Fprintln(os.Stderr, "meshapp: state:", err)
	}
}

// logEntry is one line of the call log.
type logEntry struct {
	T0      float64 `json:"t0"`
	T1      float64 `json:"t1"`
	To      string  `json:"to"`
	Kind    string  `json:"kind"`
	Outcome string  `json:"outcome"`
	Code    int     `json:"code"`
	Ms      float64 `json:"ms"`
}

// callLog appends one JSON line per finished call.
// why a log beside the aggregate: the aggregate cannot show the calls of one
// window, and a rewritten full log would write MBs per run.
type callLog struct {
	mu sync.Mutex
	f  *os.File
}

// openCallLog creates the log at path, empty.
func openCallLog(path string) (*callLog, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("call log %v: %w", path, err)
	}
	return &callLog{f: f}, nil
}

func (l *callLog) append(e logEntry) {
	raw, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// why one write per line: a reader never sees two calls' bytes interleaved
	if _, err := l.f.Write(append(raw, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "meshapp: call log:", err)
	}
}

func (l *callLog) close() error { return l.f.Close() }

// unixSeconds is t as wall-clock unix seconds.
func unixSeconds(t time.Time) float64 { return float64(t.UnixNano()) / 1e9 }
