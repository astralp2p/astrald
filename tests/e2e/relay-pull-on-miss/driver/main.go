// Command driver registers a greeter app on node2 whose relay contract node2
// does not push to node1, and calls it once from a visitor app on node1. node1
// routes the call by pulling the greeter's relay contract from node2.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// callBudget is the longest the first call may take, the pull included
// (astral-docs topics/app-routing.md, Resolving the host: the ask lasts at most 1 s).
const callBudget = 1500 * time.Millisecond

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"relay_pull_on_miss": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]any, error) {
	facts := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return facts, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	// the visitor first, so nothing happens on node1 between the greeter's
	// registration and the call
	visitor, err := apphostcli.New(nil, admin1).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the visitor on node1: %w", err)
	}
	visitorCli := connect(n1.Endpoint, string(visitor.Token))

	greeter, err := apphostcli.New(nil, admin2).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the greeter on node2: %w", err)
	}
	facts["greeter"] = greeter.Identity.String()
	stop, err := serve(ctx, n2, string(greeter.Token))
	facts["greeter_serves_on_node2"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the greeter could not serve on node2: %w", err)
	}
	defer stop()

	// the precondition: node2 pushed nothing, so node1 holds no contract
	facts["node1_held_no_contract"] = !hostsName(ctx, admin1, greeter.Identity, n2.Identity)
	if !facts["node1_held_no_contract"].(bool) {
		return facts, fmt.Errorf("apphost.hosts on node1 names node2 for the greeter before the call; the push was not dropped")
	}

	start := time.Now()
	reply, err := hello(ctx, visitorCli, greeter.Identity)
	took := time.Since(start)
	facts["call_ms"] = took.Milliseconds()
	want := "hello, " + visitor.Identity.String()
	facts["visitor_greeted"] = err == nil && reply == want
	if !facts["visitor_greeted"].(bool) {
		return facts, fmt.Errorf("the visitor on node1 called greeter:hello and got %q (err %v); want %q", reply, err, want)
	}
	facts["call_within_budget"] = took < callBudget
	if took >= callBudget {
		return facts, fmt.Errorf("the first call took %v; want under %v", took, callBudget)
	}

	facts["node1_holds_the_contract"] = hostsName(ctx, admin1, greeter.Identity, n2.Identity)
	if !facts["node1_holds_the_contract"].(bool) {
		return facts, fmt.Errorf("after the call, apphost.hosts on node1 does not name node2 for the greeter")
	}

	fmt.Fprintf(os.Stderr, "ok: node1 pulled the greeter's contract and the first call took %v\n", took)
	return facts, nil
}

// Greeter is the greeter app's API.
type Greeter struct{}

// Hello greets the caller by the identity the node delivered.
func (*Greeter) Hello(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8("hello, " + q.Caller().String()))
}

// serve runs the greeter API as the app that token authenticates, through its
// own client and registrar. It returns once the node has attached the app's
// handler, or with the error that stopped the registration.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, n node, token string) (func(), error) {
	cli := apphostcli.New(nil, connect(n.Endpoint, token))
	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(cli))
	attached := make(chan struct{})
	var once atomic.Bool
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		if once.CompareAndSwap(false, true) {
			close(attached)
		}
		return nil
	})
	// why host_ip: the node dials the handler's registered endpoint for every
	// query, and only an address at the node's host_ip reaches the test host
	// from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(n.HostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Greeter{}), reg, at, onAttach) }()

	select {
	case <-attached:
		return cancel, nil
	case err := <-served:
		cancel()
		return nil, err
	case <-time.After(30 * time.Second):
		cancel()
		return nil, errors.New("the app's handler was not attached within 30s")
	}
}

// hostsName reports whether apphost.hosts on the admin's node names host for app.
func hostsName(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
	found := false
	for h := range hosts {
		found = found || h.String() == host
	}
	return *errp == nil && found
}

// hello queries app:hello as the client's app and returns the reply.
func hello(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := caller.WithTarget(app).QueryChannel(cctx, "hello", nil)
	if err != nil {
		return "", err
	}
	defer ch.Close()
	var reply *astral.String8
	if err := ch.Switch(channel.Expect(&reply), channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF()); err != nil {
		return "", err
	}
	return reply.String(), nil
}
