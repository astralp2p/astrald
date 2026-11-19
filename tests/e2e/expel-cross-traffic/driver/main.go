// Command driver holds the apps and swarm follows that expel-cross-traffic
// watches across the ban, and probes app routing from node2 once the ban
// reached node2.
//
//	driver <session.json>
//
// It prints one JSON line {"ready": ...} once every app serves and both
// follows hold node1's offering, then one JSON line {"probe": ...} for each
// "probe" line on stdin. It ends its apps and follows and exits when stdin
// closes.
//
// why Go: astral-py speaks the pre-v1 Services wire (services-swarm-membership).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// service is the name node1's provider offers and both follows ask for.
const service = "expel-cross-traffic"

// The bounds of one probe query: a member host answers within reachWithin
// (acceptance criterion 7), and a query with no member host ends within
// refuseWithin.
const (
	reachWithin  = 5 * time.Second
	refuseWithin = 20 * time.Second
	readyWithin  = 60 * time.Second
)

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

// ready is what the driver set up, by identity.
type ready struct {
	AppNode1     string   `json:"app_node1"`
	AppNode3     string   `json:"app_node3"`
	Provider     string   `json:"provider"`
	FollowNode2  string   `json:"follow_node2"`
	FollowNode3  string   `json:"follow_node3"`
	HostsBefore3 []string `json:"hosts_before_node3_app"`
}

// probe is one probe from node2 after the ban.
type probe struct {
	HostsApp1   []string `json:"hosts_app_node1"`
	HostsApp3   []string `json:"hosts_app_node3"`
	App1Err     string   `json:"app_node1_err"`
	App1Seconds float64  `json:"app_node1_seconds"`
	App3Err     string   `json:"app_node3_err"`
	App3Seconds float64  `json:"app_node3_seconds"`
	App3Served  int64    `json:"app_node3_served_during_probe"`
	App1Served  int64    `json:"app_node1_served_during_probe"`
}

type world struct {
	ctx        *astral.Context
	n1, n2, n3 provider.Node
	admin2     *astrald.Client
	app1, app3 *pinger
	follows    []*servicescli.Watcher
	stops      []func()
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	w, err := load(path)
	if err != nil {
		return err
	}
	defer w.close()

	r, err := w.setUp()
	if err != nil {
		return err
	}
	emit("ready", r)

	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		if in.Text() == "probe" {
			emit("probe", w.probe())
		}
	}
	return nil
}

func load(path string) (*world, error) {
	s, err := provider.Load(path)
	if err != nil {
		return nil, err
	}
	w := &world{ctx: astral.NewContext(nil)}
	for name, n := range map[string]*provider.Node{"node1": &w.n1, "node2": &w.n2, "node3": &w.n3} {
		if *n, err = s.Node(name); err != nil {
			return nil, err
		}
	}
	w.admin2 = w.n2.Admin()
	return w, nil
}

func (w *world) close() {
	for _, f := range w.follows {
		_ = f.Close()
	}
	for _, stop := range slices.Backward(w.stops) {
		stop()
	}
}

// setUp serves an app on node1 and one on node3, a provider of service on
// node1, and swarm follows of service on node2 and node3, and waits until both
// follows hold the provider's offering and node2 knows node3 as the host of
// node3's app.
func (w *world) setUp() (*ready, error) {
	var err error
	if w.app1, err = w.servePinger(w.n1); err != nil {
		return nil, err
	}
	if w.app3, err = w.servePinger(w.n3); err != nil {
		return nil, err
	}
	providerID, err := w.advertise(w.n1)
	if err != nil {
		return nil, err
	}
	want := providerID.String() + "/" + service
	r := &ready{AppNode1: w.app1.id.String(), AppNode3: w.app3.id.String(), Provider: providerID.String()}
	for _, n := range []struct {
		node provider.Node
		id   *string
	}{{w.n2, &r.FollowNode2}, {w.n3, &r.FollowNode3}} {
		id, err := w.follow(n.node, want)
		if err != nil {
			return nil, err
		}
		*n.id = id.String()
	}
	if r.HostsBefore3, err = w.waitHosts(w.app3.id, w.n3.Identity); err != nil {
		return nil, err
	}
	return r, nil
}

// advertise registers a provider of service on n that offers it to every caller.
func (w *world) advertise(n provider.Node) (*astral.Identity, error) {
	app, err := provider.Register(w.ctx, n.Admin(), n, serveApps)
	if err != nil {
		return nil, err
	}
	b, err := servicescli.New(nil, app.Client).AdvertiseOpen(w.ctx, []string{service}, true,
		func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		})
	if err != nil {
		return nil, fmt.Errorf("advertise on %v: %w", n.Identity, err)
	}
	w.stops = append(w.stops, func() { _ = b.Close() })
	return app.ID, nil
}

// follow opens a swarm follow of service as a fresh app on n, granted on n,
// and waits until it holds want.
func (w *world) follow(n provider.Node, want string) (*astral.Identity, error) {
	admin := n.Admin()
	app, err := provider.Register(w.ctx, admin, n)
	if err != nil {
		return nil, err
	}
	scope := astral.NewBundle()
	rule := &services.DiscoveryRule{Services: []astral.String8{service}}
	if err := scope.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return nil, err
	}
	err = apphostcli.New(nil, admin).Grant(w.ctx, &apphostapi.GrantRequest{
		Identity: app.ID,
		Permits:  []*auth.Permit{{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: scope}},
	})
	if err != nil {
		return nil, fmt.Errorf("grant discovery on %v: %w", n.Identity, err)
	}
	watch, err := servicescli.New(nil, app.Client).WatchIn(w.ctx, services.ReachSwarm, []string{service})
	if err != nil {
		return nil, fmt.Errorf("follow on %v: %w", n.Identity, err)
	}
	w.follows = append(w.follows, watch)
	return app.ID, holds(watch, want)
}

// holds waits until the watcher holds want among its offerings.
func holds(watch *servicescli.Watcher, want string) error {
	deadline := time.After(readyWithin)
	for {
		var got []string
		for _, u := range watch.Offerings() {
			got = append(got, u.ProviderID.String()+"/"+string(u.Name))
		}
		if slices.Contains(got, want) {
			return nil
		}
		select {
		case <-watch.Changed():
		case <-watch.Done():
			return fmt.Errorf("follow ended: %v", watch.Err())
		case <-deadline:
			return fmt.Errorf("follow holds %v; want %v", got, want)
		}
	}
}

// waitHosts waits until node2's apphost.hosts names host for app.
func (w *world) waitHosts(app *astral.Identity, host string) ([]string, error) {
	deadline := time.Now().Add(readyWithin)
	for {
		got, err := w.hosts(app)
		if err == nil && slices.Contains(got, host) {
			return got, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("node2 names hosts %v (%v) for %v; want %v among them", got, err, app, host)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// hosts is node2's apphost.hosts answer for app.
func (w *world) hosts(app *astral.Identity) ([]string, error) {
	ids, errp := apphostcli.New(nil, w.admin2).Hosts(w.ctx, app.String())
	out := []string{}
	for id := range ids {
		out = append(out, id.String())
	}
	slices.Sort(out)
	return out, *errp
}

// probe asks node2's hosts of both apps, then pings both apps from node2.
func (w *world) probe() *probe {
	p := &probe{}
	p.HostsApp1, _ = w.hosts(w.app1.id)
	p.HostsApp3, _ = w.hosts(w.app3.id)
	served1, served3 := w.app1.served.Load(), w.app3.served.Load()
	p.App1Seconds, p.App1Err = w.ping(w.app1.id, reachWithin)
	p.App3Seconds, p.App3Err = w.ping(w.app3.id, refuseWithin)
	p.App1Served = w.app1.served.Load() - served1
	p.App3Served = w.app3.served.Load() - served3
	return p
}

// ping queries app's probe.ping from node2 within limit, and answers how long
// it took and the error text, empty on a pong.
func (w *world) ping(app *astral.Identity, limit time.Duration) (float64, string) {
	ctx, cancel := w.ctx.WithTimeout(limit)
	defer cancel()
	start := time.Now()
	err := pingOnce(ctx, w.admin2.WithTarget(app))
	took := time.Since(start).Seconds()
	if err != nil {
		return took, err.Error()
	}
	return took, ""
}

func emit(key string, v any) {
	out, _ := json.Marshal(map[string]any{key: v})
	fmt.Println(string(out))
}
