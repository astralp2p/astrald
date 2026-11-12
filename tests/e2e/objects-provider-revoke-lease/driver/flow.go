package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// revoke: P and Q register, P loses its grant, is refused re-registration,
// gets the grant back and registers again. Q registers twice, so its second
// registration is a renewal.
func revoke(w *world, f *facts) error {
	for _, r := range []struct {
		key string
		p   *provider.Provider
	}{{"P", w.p}, {"Q", w.q}, {"Q renewal", w.q}} {
		if err := w.register(f, r.key, r.p, auth.RoleFinder, longLease); err != nil {
			return err
		}
	}
	if err := w.find(f, "find1", "pid qid", 1, 1, 0); err != nil {
		return err
	}
	if err := provider.Revoke(w.ctx, w.user, w.p.App.ID, provider.ServeObjects); err != nil {
		return fmt.Errorf("driver wiring: revoke P's grant: %w", err)
	}
	if err := w.find(f, "find2_revoked", "qid", 1, 2, 0); err != nil {
		return err
	}
	_, err := w.p.App.RegisterAs(w.ctx, auth.RoleFinder, longLease)
	f.Refused = provider.ErrText(err)
	if err := f.checks.Check("reregister_refused", f.Refused == auth.ErrNotPermitted.Error(),
		"P's register_finder after the revoke answered %q; want %q", f.Refused, auth.ErrNotPermitted); err != nil {
		return err
	}
	if err := w.find(f, "find3_refused", "qid", 1, 3, 0); err != nil {
		return err
	}
	finder, err := provider.ServeObjectsPermit(auth.RoleFinder)
	if err != nil {
		return err
	}
	if err := provider.Grant(w.ctx, w.user, w.p.App.ID, finder); err != nil {
		return fmt.Errorf("driver wiring: grant P ServeObjects[finder] again: %w", err)
	}
	l, err := w.p.App.RegisterAs(w.ctx, auth.RoleFinder, longLease)
	f.Regrant = reply(l, err)
	if err := f.checks.Check("reregister_after_grant", err == nil && l != nil,
		"P's register_finder after the grant answered %q; want a lease", f.Regrant); err != nil {
		return err
	}
	return w.find(f, "find4_regranted", "pid qid", 2, 4, 0)
}

// finderLapse: E registers for 3 s and never renews; it is asked while the
// lease is live, not once it has lapsed, and again once it registers anew.
func finderLapse(w *world, f *facts) error {
	if err := w.register(f, "E", w.e, auth.RoleFinder, shortLease); err != nil {
		return err
	}
	granted := time.Now()
	if err := w.liveCheck(f, "E", granted); err != nil {
		return err
	}
	if err := w.find(f, "find5_e_live", "pid qid eid", 3, 5, 1); err != nil {
		return err
	}
	time.Sleep(time.Until(granted.Add(lapseWait)))
	if err := w.find(f, "find6_e_lapsed", "pid qid", 4, 6, 1); err != nil {
		return err
	}
	if err := w.register(f, "E revived", w.e, auth.RoleFinder, reLease); err != nil {
		return err
	}
	return w.find(f, "find7_e_revived", "pid qid eid", 5, 7, 2)
}

// searcherLapse: the same for Z, a searcher.
func searcherLapse(w *world, f *facts) error {
	if err := w.register(f, "Z", w.z, auth.RoleSearcher, shortLease); err != nil {
		return err
	}
	granted := time.Now()
	if err := w.liveCheck(f, "Z", granted); err != nil {
		return err
	}
	if err := w.search(f, "search1_z_live", "zx@Z", 1); err != nil {
		return err
	}
	time.Sleep(time.Until(granted.Add(lapseWait)))
	if err := w.search(f, "search2_z_lapsed", "", 1); err != nil {
		return err
	}
	if err := w.register(f, "Z revived", w.z, auth.RoleSearcher, reLease); err != nil {
		return err
	}
	return w.search(f, "search3_z_revived", "zx@Z", 2)
}

// cleanUp revokes every grant; the next find and search reach none of the
// apps, and nothing of them stays registered in the world.
func cleanUp(w *world, f *facts) error {
	for _, p := range []*provider.Provider{w.p, w.q, w.e, w.z} {
		if err := provider.Revoke(w.ctx, w.user, p.App.ID, provider.ServeObjects); err != nil {
			return fmt.Errorf("driver wiring: revoke %v: %w", p.App.ID, err)
		}
	}
	if err := w.find(f, "find8_all_revoked", "", 5, 7, 2); err != nil {
		return err
	}
	return w.search(f, "search4_all_revoked", "", 2)
}

// liveCheck requires the first ask after a short lease to start while the
// lease is live; under that, a missing answer would say nothing.
func (w *world) liveCheck(f *facts, name string, granted time.Time) error {
	time.Sleep(500 * time.Millisecond)
	return f.checks.Require(name+"_asked_while_live", time.Since(granted) < liveWithin,
		"the first ask after %s's 3 s lease started %.1f s after it; want under %v", name, time.Since(granted).Seconds(), liveWithin)
}

// register has p call the register op of role with its own token, records
// the reply under key, and requires a lease.
func (w *world) register(f *facts, key string, p *provider.Provider, role astral.String8, lease astral.Duration) error {
	l, err := p.App.RegisterAs(w.ctx, role, lease)
	f.Leases[key] = reply(l, err)
	return f.checks.Require("registered "+key, err == nil && l != nil, "register_%s answered %q", role, f.Leases[key])
}

// find runs the User's find for X, records it as step name, and checks the
// answer is exactly want (names, space separated) and P, Q and E were asked
// p, q and e times in all.
func (w *world) find(f *facts, name, want string, p, q, e int) error {
	ids, err := provider.FindAll(w.ctx, w.cli, w.x, callTimeout)
	s := step{Err: provider.ErrText(err), Counts: map[string]int{
		"P": w.p.Count(provider.Find, w.x.String()),
		"Q": w.q.Count(provider.Find, w.x.String()),
		"E": w.e.Count(provider.Find, w.x.String()),
	}}
	for _, id := range ids {
		s.Answer = append(s.Answer, w.name(id.String()))
	}
	f.Steps[name] = s
	wantCounts := map[string]int{"P": p, "Q": q, "E": e}
	return f.checks.Check(name, s.Err == "" && sameNames(s.Answer, want) && equalCounts(s.Counts, wantCounts),
		"find answered %v (err %q) with counters %v; want {%s} and %v", s.Answer, s.Err, s.Counts, want, wantCounts)
}

// search runs the User's search for the word, records it as step name with
// each result as "<object>@<source>", and checks the answer is exactly want
// and Z was asked z times in all.
func (w *world) search(f *facts, name, want string, z int) error {
	rs, err := provider.SearchAll(w.ctx, w.cli, w.word, callTimeout)
	s := step{Err: provider.ErrText(err), Counts: map[string]int{"Z": w.z.Count(provider.Search, w.word)}}
	for _, r := range rs {
		s.Answer = append(s.Answer, w.name(r.ObjectID.String())+"@"+w.name(r.SourceID.String()))
	}
	f.Steps[name] = s
	return f.checks.Check(name, s.Err == "" && sameNames(s.Answer, want) && s.Counts["Z"] == z,
		"search answered %v (err %q), Z asked %d times; want {%s} and %d", s.Answer, s.Err, s.Counts["Z"], want, z)
}

func (w *world) name(id string) string {
	if n, ok := w.names[id]; ok {
		return n
	}
	return id
}

// sameNames reports whether got holds exactly the space-separated names of
// want, in any order.
func sameNames(got []string, want string) bool {
	g, wl := slices.Clone(got), strings.Fields(want)
	slices.Sort(g)
	slices.Sort(wl)
	return slices.Equal(g, wl)
}

// reply renders one register answer: "lease <duration>" or "error: <text>".
func reply(l *objects.RegistrationLease, err error) string {
	switch {
	case err != nil:
		return "error: " + err.Error()
	case l == nil:
		return "no answer"
	}
	return "lease " + time.Duration(l.Duration).String()
}

func equalCounts(a, b map[string]int) bool {
	for k, v := range b {
		if a[k] != v {
			return false
		}
	}
	return true
}
