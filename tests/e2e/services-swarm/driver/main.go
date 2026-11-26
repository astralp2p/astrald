// Command driver exercises the Services API against two linked nodes.
//
//	driver drive  <session.json>   the v1 and swarm scenarios; prints facts as JSON
//	driver verify <session.json>   one independent swarm round trip
//	driver membership <session.json>  a swarm follow across a late join and an expulsion; prints facts as JSON
//	driver rejoin <session.json>      a swarm follow across a member's expiry and its rejoin; prints facts as JSON
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"
)

var serveApps = &auth.Permit{Action: "mod.auth.serve_apps_action"}

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

type world struct {
	ctx            *astral.Context
	n1, n2         node
	node1, node2   *astral.Identity
	admin1, admin2 *astrald.Client
	// note: the client of each app w.app registered, by identity, so
	// requestDiscovery files its request as the app
	clients sync.Map
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify|membership|rejoin <session.json>")
		os.Exit(2)
	}
	if flow, ok := map[string]func(string) error{"membership": membership, "rejoin": rejoin}[os.Args[1]]; ok {
		if err := flow(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "FAIL:", err)
			os.Exit(1)
		}
		return
	}
	w, err := load(os.Args[2])
	if err == nil {
		switch os.Args[1] {
		case "drive":
			err = drive(w)
		case "verify":
			err = verify(w)
		default:
			err = fmt.Errorf("unknown mode %q", os.Args[1])
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func load(path string) (*world, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	w := &world{ctx: astral.NewContext(nil), n1: s.Nodes["node1"], n2: s.Nodes["node2"]}
	if w.node1, err = astral.ParseIdentity(w.n1.Identity); err != nil {
		return nil, err
	}
	if w.node2, err = astral.ParseIdentity(w.n2.Identity); err != nil {
		return nil, err
	}
	w.admin1, w.admin2 = connect(w.n1.Endpoint, w.n1.Token), connect(w.n2.Endpoint, w.n2.Token)
	return w, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// app registers a fresh app identity on a node, waits for its contract, and
// returns its services client.
func (w *world) app(admin *astrald.Client, n node, permits ...*auth.Permit) (*astral.Identity, *servicescli.Client, error) {
	tok, err := contracts.Register(w.ctx, admin, n.Endpoint, permits...)
	if err != nil {
		return nil, nil, err
	}
	c := connect(n.Endpoint, string(tok.Token))
	w.clients.Store(tok.Identity.String(), c)
	return tok.Identity, servicescli.New(nil, c), nil
}

// requestDiscovery has app request a discovery permit for names on nodes (any
// node when empty), and waits for its contract.
func (w *world) requestDiscovery(app *astral.Identity, names []string, nodes ...*astral.Identity) error {
	rule := &services.DiscoveryRule{Nodes: nodes}
	for _, n := range names {
		rule.Services = append(rule.Services, astral.String8(n))
	}
	cs := astral.NewBundle()
	if err := cs.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return err
	}
	c, ok := w.clients.Load(app.String())
	if !ok {
		return fmt.Errorf("no client of %v", app)
	}
	_, err := contracts.Request(w.ctx, c.(*astrald.Client),
		&auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs})
	return err
}

// evaluated records the callers a provider evaluated.
type evaluated struct {
	mu      sync.Mutex
	callers map[string]bool
	gen     atomic.Int64
}

func (e *evaluated) offer(_ *astral.Context, caller *astral.Identity, _ string) (*services.Update, error) {
	e.mu.Lock()
	if e.callers == nil {
		e.callers = map[string]bool{}
	}
	e.callers[caller.String()] = true
	e.mu.Unlock()
	info := astral.NewBundle()
	_ = info.Append(astral.NewString8(fmt.Sprintf("gen-%d", e.gen.Load())))
	return &services.Update{Available: true, Info: info}, nil
}

func (e *evaluated) saw(id *astral.Identity) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.callers[id.String()]
}

// result is a one-shot discovery read to its end.
type result struct {
	providers  []string
	notGranted []string
	outcome    *servicescli.InitialOutcome
	err        error
}

func oneShot(events <-chan servicescli.Event, err error) result {
	if err != nil {
		return result{err: err}
	}
	var r result
	for ev := range events {
		switch {
		case ev.Update != nil:
			r.providers = append(r.providers, ev.Update.ProviderID.String()+"/"+string(ev.Update.Name))
		case ev.NotGranted != nil:
			r.notGranted = ev.NotGranted
		case ev.Initial != nil:
			r.outcome = ev.Initial
		case ev.Err != nil:
			r.err = ev.Err
		}
	}
	sort.Strings(r.providers)
	return r
}

func (r result) complete() bool { return r.err == nil && r.outcome != nil && r.outcome.Complete }

func keys(ids ...string) string {
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// holds waits until the watcher holds exactly want.
func holds(w *servicescli.Watcher, want ...string) error {
	return holdsWithin(w, 20*time.Second, want...)
}

// holdsWithin waits up to wait until the watcher holds exactly want.
func holdsWithin(w *servicescli.Watcher, wait time.Duration, want ...string) error {
	deadline := time.After(wait)
	for {
		var got []string
		for _, u := range w.Offerings() {
			got = append(got, u.ProviderID.String()+"/"+string(u.Name))
		}
		if keys(got...) == keys(want...) {
			return nil
		}
		select {
		case <-w.Changed():
		case <-w.Done():
			return fmt.Errorf("follow ended: %v", w.Err())
		case <-deadline:
			return fmt.Errorf("follow holds %v; want %v", got, want)
		}
	}
}

var errStep = errors.New("step failed")

func step(facts map[string]bool, name string, err error) error {
	facts[name] = err == nil
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintln(os.Stderr, "ok:", name)
	return nil
}

func expect(ok bool, format string, args ...any) error {
	if ok {
		return nil
	}
	return fmt.Errorf("%w: "+format, append([]any{errStep}, args...)...)
}
