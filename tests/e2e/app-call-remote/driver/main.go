// Command driver runs a greeter app on node2 and a visitor app on node1, each
// on its own client and token. The visitor calls the greeter by its app
// identity across the link and is greeted as itself.
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

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

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
		out, _ := json.Marshal(map[string]any{"app_call_remote": facts})
		fmt.Println(string(out))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

func run(path string) (map[string]bool, error) {
	facts := map[string]bool{}
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

	// the greeter app on node2: its own identity, token and client, serving hello
	greeter, err := contracts.Register(ctx, admin2, n2.Endpoint)
	if err != nil {
		return facts, fmt.Errorf("registering the greeter on node2: %w", err)
	}
	stop, err := serve(ctx, n2, string(greeter.Token))
	facts["greeter_serves_on_node2"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the greeter could not serve on node2: %w", err)
	}
	defer stop()

	// the visitor app on node1: another identity, token and client
	visitor, err := contracts.Register(ctx, admin1, n1.Endpoint)
	if err != nil {
		return facts, fmt.Errorf("registering the visitor on node1: %w", err)
	}
	visitorCli := connect(n1.Endpoint, string(visitor.Token))

	reply, err := hello(ctx, visitorCli, greeter.Identity)
	want := "hello, " + visitor.Identity.String()
	facts["visitor_greeted_across_nodes"] = err == nil && reply == want
	if !facts["visitor_greeted_across_nodes"] {
		return facts, fmt.Errorf("the visitor on node1 called greeter:hello and got %q (err %v); want %q", reply, err, want)
	}

	// why after the call: node1 holds the greeter's relay contract once it
	// routed the call, from the registration push or from its own pull.
	facts["node1_knows_the_greeter"] = hostsName(ctx, admin1, greeter.Identity, n2.Identity)
	if !facts["node1_knows_the_greeter"] {
		return facts, fmt.Errorf("after the call, apphost.hosts on node1 does not name node2 for the greeter %v", greeter.Identity)
	}

	fmt.Fprintln(os.Stderr, "ok: the visitor on node1 called the greeter on node2 and was greeted as itself")
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
