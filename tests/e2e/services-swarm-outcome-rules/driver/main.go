// Command driver exercises discovery outcomes across a two-node swarm and a
// linked stranger.
//
//	driver drive  <session.json>   observe the outcomes; prints them as JSON facts
//	driver verify <session.json>   observe them again with fresh identities; prints JSON
//
// The driver judges nothing: verify.py reads what both modes observed.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/astrald"
)

const (
	open = "ssor-open" // answered by node2's provider
	slow = "ssor-slow" // node2's provider never answers; it stays bound
	gone = "ssor-gone" // node2's provider never answers; it leaves while asked

	// why a deadline: a discovery that is never answered would hang the run;
	// the node's own initial budget is 5 s, so a healthy one ends well inside.
	deadline = 30 * time.Second
)

var serveApps = &auth.Permit{Action: "mod.auth.serve_apps_action"}

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
}

type world struct {
	n1, n2, s      node
	node1, node2   *astral.Identity
	stranger       *astral.Identity
	user           *astral.Identity
	admin1, admin2 *astrald.Client
}

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "drive" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	w, err := load(os.Args[2])
	var obs *observation
	if err == nil {
		obs, err = observe(w)
	}
	if obs != nil {
		out := any(obs)
		if os.Args[1] == "drive" {
			out = map[string]any{"services_swarm_outcome_rules": obs}
		}
		raw, _ := json.Marshal(out)
		fmt.Println(string(raw))
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
		Facts struct {
			UserID string `json:"user_id"`
		} `json:"facts"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	w := &world{n1: s.Nodes["node1"], n2: s.Nodes["node2"], s: s.Nodes["ssor_stranger"]}
	for id, dst := range map[string]**astral.Identity{
		w.n1.Identity: &w.node1, w.n2.Identity: &w.node2, w.s.Identity: &w.stranger, s.Facts.UserID: &w.user,
	} {
		if *dst, err = astral.ParseIdentity(id); err != nil {
			return nil, fmt.Errorf("session identity %q: %w", id, err)
		}
	}
	w.admin1, w.admin2 = connect(w.n1), connect(w.n2)
	return w, nil
}

func connect(n node) *astrald.Client { return astrald.New(apphost.NewRouter(n.Endpoint, n.Token)) }

// app registers a fresh app identity through admin and returns its services
// client on n.
func app(admin *astrald.Client, n node, permits ...*auth.Permit) (*astral.Identity, *servicescli.Client, error) {
	ctx := astral.NewContext(nil)
	tok, err := apphostcli.New(nil, admin).Register(ctx, permits...)
	if err != nil {
		return nil, nil, err
	}
	return tok.Identity, servicescli.New(nil, apphostClient(n.Endpoint, string(tok.Token))), nil
}

func apphostClient(endpoint, token string) *astrald.Client {
	return astrald.New(apphost.NewRouter(endpoint, token))
}

// grantDiscovery lets app discover names on any node, on node1.
func (w *world) grantDiscovery(app *astral.Identity, names ...string) error {
	rule := &services.DiscoveryRule{}
	for _, n := range names {
		rule.Services = append(rule.Services, astral.String8(n))
	}
	cs := astral.NewBundle()
	if err := cs.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return err
	}
	return apphostcli.New(nil, w.admin1).Grant(astral.NewContext(nil), &apphostapi.GrantRequest{
		Identity: app,
		Permits:  []*auth.Permit{{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}},
	})
}

// outcome is one discovery read to its end: what arrived, in a form the
// oracle compares.
type outcome struct {
	Providers  []string `json:"providers"`       // "<provider>/<service>", sorted
	Opens      []bool   `json:"opens,omitempty"` // visibility tags of a member's tagged updates
	NotGranted []string `json:"not_granted"`     // services.not_granted
	Complete   bool     `json:"complete"`        // a bare eos
	Incomplete []string `json:"incomplete"`      // services.incomplete
	Error      string   `json:"error"`           // the stream's terminal error or the query's
	ElapsedMS  int64    `json:"elapsed_ms"`
}

func read(events <-chan servicescli.Event, err error, start time.Time) outcome {
	o := outcome{Providers: []string{}, NotGranted: []string{}, Incomplete: []string{}}
	if err != nil {
		o.Error = err.Error()
		return o
	}
	for ev := range events {
		switch {
		case ev.Update != nil:
			o.Providers = append(o.Providers, ev.Update.ProviderID.String()+"/"+string(ev.Update.Name))
			if ev.Open != nil {
				o.Opens = append(o.Opens, *ev.Open)
			}
		case ev.NotGranted != nil:
			o.NotGranted = ev.NotGranted
		case ev.Initial != nil:
			o.Complete = ev.Initial.Complete
			if ev.Initial.Incomplete != nil {
				o.Incomplete = ev.Initial.Incomplete
			}
		case ev.Err != nil:
			o.Error = ev.Err.Error()
		}
	}
	sort.Strings(o.Providers)
	o.ElapsedMS = time.Since(start).Milliseconds()
	return o
}

// discover runs one discovery with a deadline.
func discover(c *servicescli.Client, run func(*astral.Context, *servicescli.Client) (<-chan servicescli.Event, error)) outcome {
	ctx, cancel := astral.NewContext(nil).WithTimeout(deadline)
	defer cancel()
	start := time.Now()
	events, err := run(ctx, c)
	return read(events, err, start)
}
