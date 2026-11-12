// Command driver revokes and lets lapse object-provider registrations on
// node1 and records what the User's find and search then reach. P and Q are
// finders; P loses its grant, is refused re-registration, gets the grant back
// and registers again, while Q stays. E is a finder and Z a searcher whose
// 3 s leases lapse unrenewed and revive on re-registration.
//
//	driver drive  <session.json>   records facts as JSON; stops only on a wiring fault
//	driver verify <session.json>   the same flow, with fresh apps, every check binding
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/objects"
	objectscli "github.com/astralp2p/astral-go/api/objects/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

const (
	longLease  = astral.Duration(120 * time.Second) // P and Q
	shortLease = astral.Duration(3 * time.Second)   // E and Z, never renewed
	reLease    = astral.Duration(60 * time.Second)  // E and Z, revived
	// lapseWait is when, after a short lease was granted, the flow asks
	// again: past the 3 s lease and the world's 1 s sweep.
	lapseWait = 5 * time.Second
	// liveWithin is how soon after a short lease was granted the first ask
	// must start, so the lease is still live under it.
	liveWithin  = 2 * time.Second
	callTimeout = 30 * time.Second
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	f := &facts{Steps: map[string]step{}, checks: provider.NewChecks(os.Args[1])}
	f.Checks = f.checks.Held
	err := run(os.Args[2], f)
	if os.Args[1] == "drive" {
		provider.Emit("objects_provider_revoke_lease", f)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// step is what one find or search answered, by name, and every app's counter
// after it.
type step struct {
	Answer []string       `json:"answer"`
	Err    string         `json:"err"`
	Counts map[string]int `json:"counts"`
}

// facts is what the driver records.
type facts struct {
	X      string            `json:"x"`      // the object every find asks for
	Word   string            `json:"word"`   // the text every search asks for
	Apps   map[string]string `json:"apps"`   // P, Q, E, Z -> app identity
	Claims map[string]string `json:"claims"` // identity -> pid, qid, eid, zx (an object id)

	Steps   map[string]step   `json:"steps"`
	Refused string            `json:"refused_reregister"` // P's register_finder after the revoke
	Regrant string            `json:"regrant_reregister"` // P's register_finder after the grant
	Leases  map[string]string `json:"leases"`             // each other register reply

	Checks map[string]bool `json:"checks"`
	checks *provider.Checks
}

// world is the flow's cast on node1.
type world struct {
	ctx        *astral.Context
	user       *astrald.Client
	cli        *objectscli.Client
	x          *astral.ObjectID
	word       string
	p, q, e, z *provider.Provider
	names      map[string]string // identity or object id -> name
	stops      []func()
}

func (w *world) close() {
	for _, p := range []*provider.Provider{w.p, w.q, w.e, w.z} {
		if p != nil {
			_ = provider.Revoke(w.ctx, w.user, p.App.ID, provider.ServeObjects)
		}
	}
	for _, stop := range w.stops {
		stop()
	}
}

func run(path string, f *facts) error {
	w, err := setUp(path, f)
	if w != nil {
		defer w.close()
	}
	if err != nil {
		return err
	}
	for _, phase := range []func(*world, *facts) error{revoke, finderLapse, searcherLapse, cleanUp} {
		if err := phase(w, f); err != nil {
			return err
		}
	}
	return nil
}

func setUp(path string, f *facts) (*world, error) {
	s, err := provider.Load(path)
	if err != nil {
		return nil, err
	}
	n1, err := s.Node("node1")
	if err != nil {
		return nil, err
	}
	token, err := s.UserToken()
	if err != nil {
		return nil, err
	}
	w := &world{ctx: astral.NewContext(nil), user: provider.Connect(n1.Endpoint, token), names: map[string]string{}}
	w.cli = objectscli.New(nil, w.user)
	nonce := astral.NewNonce().String()
	w.word = "objects-provider-revoke-lease-" + nonce
	if w.x, err = astral.ResolveObjectID(astral.NewString16("objects-provider-revoke-lease X " + nonce)); err != nil {
		return w, err
	}
	zx, err := astral.ResolveObjectID(astral.NewString16("objects-provider-revoke-lease ZX " + nonce))
	if err != nil {
		return w, err
	}
	finder, err := provider.ServeObjectsPermit(auth.RoleFinder)
	if err != nil {
		return w, err
	}
	searcher, err := provider.ServeObjectsPermit(auth.RoleSearcher)
	if err != nil {
		return w, err
	}
	for _, a := range []struct {
		slot   **provider.Provider
		name   string
		permit *auth.Permit
	}{{&w.p, "P", finder}, {&w.q, "Q", finder}, {&w.e, "E", finder}, {&w.z, "Z", searcher}} {
		if *a.slot, err = w.serve(n1.Endpoint, a.name, a.permit); err != nil {
			return w, fmt.Errorf("app %s: %w", a.name, err)
		}
	}
	w.z.SetResults(&objects.SearchResult{ObjectID: zx})
	w.names[zx.String()] = "zx"
	f.X, f.Word = w.x.String(), w.word
	f.Apps = map[string]string{"P": w.p.App.ID.String(), "Q": w.q.App.ID.String(),
		"E": w.e.App.ID.String(), "Z": w.z.App.ID.String()}
	f.Claims = map[string]string{}
	for k, v := range w.names {
		if v == "pid" || v == "qid" || v == "eid" || v == "zx" {
			f.Claims[k] = v
		}
	}
	f.Leases = map[string]string{}
	return w, nil
}

// serve registers an app with permit as the User and serves it. A finder
// claims an identity of its own, named after the app, for any object.
func (w *world) serve(endpoint, name string, permit *auth.Permit) (*provider.Provider, error) {
	app, err := provider.Register(w.ctx, w.user, endpoint, permit)
	if err != nil {
		return nil, err
	}
	p := provider.New(app)
	w.names[app.ID.String()] = name
	if name != "Z" {
		claim := astral.GenerateIdentity()
		w.names[claim.String()] = map[string]string{"P": "pid", "Q": "qid", "E": "eid"}[name]
		p.SetMode(provider.Find, provider.Claim)
		p.SetClaims(claim)
	}
	stop, err := p.Serve(w.ctx)
	if err != nil {
		return nil, err
	}
	w.stops = append(w.stops, stop)
	return p, nil
}
