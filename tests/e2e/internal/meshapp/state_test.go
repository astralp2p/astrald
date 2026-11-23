package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

var (
	selfIdentity  = astral.GenerateIdentity()
	otherIdentity = astral.GenerateIdentity()
	selfID        = selfIdentity.String()
	otherID       = otherIdentity.String()
)

func readState(t *testing.T, path string) appState {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st appState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("state file %s is not JSON: %v", path, err)
	}
	return st
}

func TestRecorderTracksListing(t *testing.T) {
	r := newRecorder(appState{})
	if !r.report(kindApp, otherID, 10, false) {
		t.Fatal("a provider first seen is not first")
	}
	if r.report(kindApp, otherID, 11, false) {
		t.Fatal("a provider reported twice is first again")
	}
	r.remove(otherID, 12, "member_lost")
	r.remove(otherID, 13, "member_left") // note: a removed offering is not removed again
	if r.callable(kindApp, otherID, false) || !r.callable(kindApp, otherID, true) {
		t.Error("a removed provider is callable only with keep-removed")
	}
	if r.report(kindApp, otherID, 14, false) {
		t.Error("a restored provider is first")
	}
	p := r.st.Providers[otherID]
	if p.ReportedAt != 10 || !p.Listed || len(p.Removed) != 1 || p.Removed[0] != (removal{T: 12, Cause: "member_lost"}) {
		t.Errorf("provider %+v; want reported at 10, listed, removed once at 12 as member_lost", p)
	}
	if len(p.Restored) != 1 || p.Restored[0] != 14 {
		t.Errorf("restored %v; want [14]", p.Restored)
	}
	if r.callable(kindProbe, otherID, true) {
		t.Error("a provider is callable as a probe")
	}
}

func TestRecorderFoldsCalls(t *testing.T) {
	r := newRecorder(appState{})
	r.report(kindProbe, otherID, 1, true)
	r.started(kindProbe, otherID, 2)
	if s := r.st.Probes[otherID].InflightSince; s == nil || *s != 2 {
		t.Fatalf("inflight_since %v; want 2", s)
	}
	fail := callRecord{Start: 2, End: 3, Outcome: outcomeUnavailable, Code: 7, Ms: 1000}
	ok1 := callRecord{Start: 4, End: 4.5, Outcome: outcomeOK, Ms: 500}
	ok2 := callRecord{Start: 5, End: 5.2, Outcome: outcomeOK, Ms: 200}
	for _, c := range []callRecord{fail, ok1, ok2} {
		r.finished(kindProbe, otherID, c)
	}
	p := r.st.Probes[otherID]
	if p.InflightSince != nil || p.Calls != 3 || p.Ok != 2 || p.MaxMs != 1000 || !p.Target {
		t.Errorf("probe %+v; want 3 calls, 2 ok, max 1000 ms, a target, none in flight", p)
	}
	if *p.First != fail || *p.LastOk != ok2 || *p.LastFail != fail {
		t.Errorf("first %+v last_ok %+v last_fail %+v", p.First, p.LastOk, p.LastFail)
	}
	if p.ByOutcome[outcomeOK] != 2 || p.ByOutcome[outcomeUnavailable] != 1 {
		t.Errorf("by_outcome %v; want ok 2, unavailable 1", p.ByOutcome)
	}
	r.finished(kindApp, otherID, ok1) // note: an unknown peer is ignored
	if len(r.st.Providers) != 0 {
		t.Errorf("a call to an unknown provider created %v", r.st.Providers)
	}
}

func TestStateWriterRenamesAndThrottles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.state.json")
	r := newRecorder(appState{App: selfID})
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		(&stateWriter{path: path, every: time.Hour, rec: r}).run(stop)
	}()
	r.update(func(st *appState) { st.Attaches = 1 })
	deadline := time.Now().Add(5 * time.Second)
	for _, err := os.Stat(path); err != nil; _, err = os.Stat(path) {
		if time.Now().After(deadline) {
			t.Fatal("the first change was never written")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for i := 2; i <= 50; i++ {
		r.update(func(st *appState) { st.Attaches = i })
	}
	time.Sleep(50 * time.Millisecond)
	if st := readState(t, path); st.Attaches != 1 || st.App != selfID {
		t.Errorf("within the interval the file shows %d attaches; want 1, the first write only", st.Attaches)
	}
	close(stop)
	<-done
	if st := readState(t, path); st.Attaches != 50 {
		t.Errorf("after stop the file shows %d attaches; want 50", st.Attaches)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("the temporary file stays: %v", err)
	}
}

func TestStateWriterWritesEmptyMapsAsObjects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.state.json")
	(&stateWriter{path: path, every: time.Second, rec: newRecorder(appState{})}).write()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"served", "providers", "probes"} {
		if _, ok := st[k].(map[string]any); !ok {
			t.Errorf("%s is %v; want an object", k, st[k])
		}
	}
	for _, k := range []string{"registered_at", "advertised_at", "initial"} {
		if v, ok := st[k]; !ok || v != nil {
			t.Errorf("%s is %v (present %v); want null", k, v, ok)
		}
	}
}

func TestCallLogAppendsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.state.json.calls.jsonl")
	if err := os.WriteFile(path, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := openCallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []logEntry{
		{T0: 1, T1: 2, To: otherID, Kind: kindApp, Outcome: outcomeOK, Ms: 1000},
		{T0: 3, T1: 4, To: otherID, Kind: kindProbe, Outcome: outcomeUnavailable, Code: 7, Ms: 1000},
	}
	for _, e := range want {
		l.append(e)
	}
	if err := l.close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []logEntry
	for s := bufio.NewScanner(f); s.Scan(); {
		var e logEntry
		if err := json.Unmarshal(s.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", s.Text(), err)
		}
		got = append(got, e)
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("log %+v; want %+v (a fresh log, the stale line gone)", got, want)
	}
}

// testMesh is a mesh whose calls cannot reach any node.
func testMesh() *mesh {
	dead := astrald.New(apphost.NewRouter("tcp:127.0.0.1:1", "none"))
	return &mesh{rec: newRecorder(appState{}), client: dead, self: selfID, service: "s",
		period: 0, probeOp: "apphost.whoami", callTimeout: time.Second}
}

func TestApplyFollowEvents(t *testing.T) {
	self, other := selfIdentity, otherIdentity
	m := testMesh()
	// why canceled: a spawned caller ends at once and records nothing
	ctx, cancel := astral.NewContext(nil).WithCancel()
	cancel()
	var wg sync.WaitGroup
	events := []servicescli.Event{
		{NotGranted: []string{}},
		{Update: &services.Update{Available: true, Name: "s", ProviderID: self}},
		{Update: &services.Update{Available: true, Name: "elsewhere", ProviderID: other}},
		{Update: &services.Update{Available: true, Name: "s", ProviderID: other}},
		{Initial: &servicescli.InitialOutcome{Complete: true}},
		{Removed: []*services.OfferingKey{{ProviderID: other, Name: "s"}}, RemovedCause: services.CauseMemberLost},
		{Update: &services.Update{Available: true, Name: "s", ProviderID: other}},
		{Update: &services.Update{Available: false, Name: "s", ProviderID: other}},
	}
	for i, ev := range events {
		m.apply(ctx, &wg, ev, float64(i))
	}
	wg.Wait()
	st := m.rec.st
	if len(st.Providers) != 1 || st.Providers[otherID] == nil {
		t.Fatalf("providers %v; want the other provider of s alone", st.Providers)
	}
	p := st.Providers[otherID]
	want := []removal{{T: 5, Cause: "member_lost"}, {T: 7, Cause: causeUnavailable}}
	if p.ReportedAt != 3 || p.Listed || len(p.Removed) != 2 || p.Removed[0] != want[0] || p.Removed[1] != want[1] {
		t.Errorf("provider %+v; want reported at 3, removed %v", p, want)
	}
	if len(p.Restored) != 1 || p.Restored[0] != 6 || p.Calls != 0 {
		t.Errorf("restored %v, %d calls; want [6] and no call recorded", p.Restored, p.Calls)
	}
	if st.FollowOpens != 1 || st.Initial == nil || !st.Initial.Complete || st.Initial.T != 4 {
		t.Errorf("follow_opens %d initial %+v; want one complete boundary at 4", st.FollowOpens, st.Initial)
	}
	m.apply(ctx, &wg, servicescli.Event{Err: context.Canceled}, 8)
	if m.rec.st.FollowErr != "" {
		t.Errorf("the app's own stop is a follow error %q", m.rec.st.FollowErr)
	}
}

func TestCauseName(t *testing.T) {
	if got := causeName(0); got != "none" {
		t.Errorf("causeName(0) = %q; want none", got)
	}
	if got := causeName(services.CauseMemberLeft); got != "member_left" {
		t.Errorf("causeName(member_left) = %q", got)
	}
}
