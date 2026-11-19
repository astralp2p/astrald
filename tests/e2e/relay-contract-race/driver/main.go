// Command driver runs the relay-contract-race story on four swarm nodes and
// prints its facts as one JSON line.
//
// No node pushes a relay contract at apphost.register (skip_sibling_push), and
// every pair of nodes is linked before the apps register, so the first-link
// sync carries none either. Each node runs a consumer app that follows the
// service with reach=swarm, then a provider app that advertises it. On the
// first available update for each remote provider, the consumer calls it once,
// with no retry: 12 ordered pairs, each routed by the consumer node's pull on
// miss.
//
//	driver drive  <session.json>   adopts node3 and node4, then the story; prints facts
//	driver verify <session.json>   the story again with fresh identities
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

const (
	// service is advertised by this test's providers alone.
	service = "relay-race"
	// callBudget is the longest a first call may take, the pull included
	// (astral-docs topics/app-routing.md, Resolving the host: the ask lasts at most 1 s).
	callBudget = 1500 * time.Millisecond
	// converge bounds every wait for an asynchronous push, link or update.
	converge = 30 * time.Second
	// settle lets the first-link syncs of the new links end before any app
	// registers.
	// why: a sync still running when a provider registers would carry the
	// provider's contract, and the call would need no pull.
	settle = 2 * time.Second
)

// names is the roster, in order.
var names = []string{"node1", "node2", "node3", "node4"}

// report is the facts of one run of the story.
type report struct {
	Steps     map[string]bool     `json:"steps"`
	Providers map[string]string   `json:"providers"` // node name -> provider app identity
	Consumers map[string]string   `json:"consumers"` // node name -> consumer app identity
	Pairs     []pair              `json:"pairs"`
	Served    map[string][]string `json:"served"` // node name -> the callers its provider served
	// HostsMissing lists every remote provider whose node apphost.hosts on a
	// consumer's node does not name after the calls.
	HostsMissing []string `json:"hosts_missing"`
}

// pair is one consumer's one call to one remote provider.
type pair struct {
	Consumer string `json:"consumer"` // node name
	Provider string `json:"provider"` // node name
	Ms       int64  `json:"ms"`
	Answered bool   `json:"answered"`
	Err      string `json:"err,omitempty"`
}

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "drive" && os.Args[1] != "verify") {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	r := &report{Steps: map[string]bool{}, Providers: map[string]string{},
		Consumers: map[string]string{}, Served: map[string][]string{}}
	err := run(os.Args[1], os.Args[2], r)
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"relay_contract_race": r})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func run(mode, path string, r *report) error {
	s, err := loadSession(path)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)

	if mode == "drive" {
		err = joinSwarm(ctx, s)
		if err := step(r, "node3_and_node4_adopted", err == nil, "%v", err); err != nil {
			return err
		}
	}
	err = awaitMesh(ctx, s)
	if err := step(r, "four_nodes_linked_pairwise", err == nil, "%v", err); err != nil {
		return err
	}
	time.Sleep(settle)

	if err := story(ctx, s, r); err != nil {
		return err
	}
	// why only in verify mode: in drive mode the oracle judges the facts, so a
	// misrouted call is a verify failure, not a driver one
	if mode == "verify" {
		return judge(r)
	}
	return nil
}

// step records a named step and returns an error when it did not hold.
func step(r *report, name string, ok bool, format string, args ...any) error {
	r.Steps[name] = ok
	if !ok {
		return fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	}
	fmt.Fprintln(os.Stderr, "ok:", name)
	return nil
}

// judge returns an error unless every one of the 12 pairs was answered once
// within the budget, every provider served each remote consumer once, and
// every consumer's node names each remote provider's host.
func judge(r *report) error {
	want := len(names) * (len(names) - 1)
	if len(r.Pairs) != want {
		return fmt.Errorf("%d pairs called; want %d: %+v", len(r.Pairs), want, r.Pairs)
	}
	for _, p := range r.Pairs {
		if !p.Answered || p.Err != "" {
			return fmt.Errorf("%s -> %s was not answered: %q", p.Consumer, p.Provider, p.Err)
		}
		if time.Duration(p.Ms)*time.Millisecond >= callBudget {
			return fmt.Errorf("%s -> %s took %d ms; want under %v", p.Consumer, p.Provider, p.Ms, callBudget)
		}
	}
	for _, name := range names {
		if got := len(r.Served[name]); got != len(names)-1 {
			return fmt.Errorf("the provider on %s served %d calls; want %d: %v", name, got, len(names)-1, r.Served[name])
		}
	}
	if len(r.HostsMissing) > 0 {
		return fmt.Errorf("hosts missing after the calls: %v", r.HostsMissing)
	}
	fmt.Fprintf(os.Stderr, "ok: %d pairs answered on the first call\n", want)
	return nil
}
