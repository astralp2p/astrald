// Command driver runs a provider app on node2 whose op rejects every query
// with code 7, and queries it from an app on node2 and from an app on node1,
// each on its own client and token.
//
//	driver <session.json>   prints facts as JSON
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
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// rejectCode is the code the provider rejects with. It is neither the default
// code (1) nor one a node picks on its own, so a caller that reads it read the
// provider's choice.
const rejectCode = 7

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[1])
	out, _ := json.Marshal(map[string]any{"app_reject_code": facts})
	fmt.Println(string(out))
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

	// the provider app on node2, with the explicit serve_apps permit
	provider, err := apphostcli.New(nil, admin2).Register(ctx, &auth.Permit{Action: "mod.auth.serve_apps_action"})
	if err != nil {
		return facts, fmt.Errorf("registering the provider on node2: %w", err)
	}
	stop, err := serve(ctx, n2, string(provider.Token))
	facts["provider_serves_on_node2"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the provider could not serve on node2: %w", err)
	}
	defer stop()

	// note: node1 routes to the provider through its relay contract, which
	// apphost.register pushes to the siblings in the background; waiting for
	// apphost.hosts keeps a route_not_found race out of the call.
	facts["node1_knows_the_provider"] = waitHost(ctx, admin1, provider.Identity, n2.Identity)
	if !facts["node1_knows_the_provider"].(bool) {
		return facts, fmt.Errorf("apphost.hosts on node1 never named node2 for the provider %v", provider.Identity)
	}

	// the caller on node2, then the caller on node1: each its own app
	callers := []struct {
		name   string
		admin  *astrald.Client
		n      node
		remote bool
	}{
		{"local", admin2, n2, false},
		{"remote", admin1, n1, true},
	}
	for _, c := range callers {
		app, err := apphostcli.New(nil, c.admin).Register(ctx)
		if err != nil {
			return facts, fmt.Errorf("registering the %s caller: %w", c.name, err)
		}
		err = refused(ctx, connect(c.n.Endpoint, string(app.Token)), provider.Identity, c.remote)
		var rejected *astral.ErrRejected
		facts[c.name+"_rejected"] = errors.As(err, &rejected)
		if rejected != nil {
			facts[c.name+"_code"] = rejected.Code
		}
		if err != nil {
			facts[c.name+"_error"] = err.Error()
		} else {
			facts[c.name+"_error"] = "the query was accepted"
		}
	}

	fmt.Fprintln(os.Stderr, "ok: both callers' queries ended; the oracle reads the codes")
	return facts, nil
}

// Provider is the provider app's API.
type Provider struct{}

// RejectCodeSeven rejects the query with code 7.
func (*Provider) RejectCodeSeven(_ *astral.Context, q *routing.IncomingQuery) error {
	return q.RejectWithCode(rejectCode)
}

// serve runs the provider API as the app that token authenticates, through its
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
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Provider{}), reg, at, onAttach) }()

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

// waitHost polls apphost.hosts on the admin's node until it names host for
// app, within 30 seconds.
func waitHost(ctx *astral.Context, admin *astrald.Client, app *astral.Identity, host string) bool {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		hosts, errp := apphostcli.New(nil, admin).Hosts(ctx, app.String())
		found := false
		for h := range hosts {
			found = found || h.String() == host
		}
		if *errp == nil && found {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// refused queries app:reject_code_seven as the client's app and returns the
// error the query ended with; nil means the node accepted it.
func refused(ctx *astral.Context, caller *astrald.Client, app *astral.Identity, remote bool) error {
	cctx := ctx
	if remote {
		cctx = ctx.IncludeZone(astral.ZoneNetwork)
	}
	cctx, cancel := cctx.WithTimeout(10 * time.Second)
	defer cancel()
	conn, err := caller.WithTarget(app).Query(cctx, "reject_code_seven", nil)
	if err == nil {
		conn.Close()
	}
	return err
}
