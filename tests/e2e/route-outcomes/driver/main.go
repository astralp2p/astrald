// Command driver queries, as an app on node1, targets that fail for different
// causes, and records the answer each query ended with and how long it took.
//
//	driver <session.json>   prints facts as JSON
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// service is the name the provider advertises; unique to this test.
const service = "route-outcomes-echo"

type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

// outcome is how one query ended.
type outcome struct {
	Kind      string  `json:"kind"` // accepted, rejected, or the apphost error code
	Code      int     `json:"code"` // the reject code when Kind is rejected
	Retryable bool    `json:"retryable"`
	Millis    float64 `json:"millis"`
	Error     string  `json:"error"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: driver <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[1])
	out, _ := json.Marshal(map[string]any{"route_outcomes": facts})
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
	node2, err := astral.ParseIdentity(n2.Identity)
	if err != nil {
		return facts, err
	}
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	// the calling app on node1
	app, err := contracts.Register(ctx, admin1, n1.Endpoint)
	if err != nil {
		return facts, fmt.Errorf("registering the caller on node1: %w", err)
	}
	// why: node2 admits a relay for the app only once it holds the app's
	// relay contract, which apphost.register pushes in the background; before
	// that every query from the app reads as node2's relay refusal.
	facts["node2_knows_the_caller"] = waitHost(ctx, admin2, app.Identity, n1.Identity)
	if !facts["node2_knows_the_caller"].(bool) {
		return facts, fmt.Errorf("apphost.hosts on node2 never named node1 for the caller %v", app.Identity)
	}
	caller := connect(n1.Endpoint, string(app.Token))
	as := asker{endpoint: n1.Endpoint, token: string(app.Token)}

	facts["unknown_identity"] = ask(ctx, as, astral.GenerateIdentity(), "route.outcomes")
	facts["no_such_op"] = ask(ctx, as, node2, "no.such.op")
	facts["refused"] = ask(ctx, as, node2, "shell.shell")
	facts["filtered"] = ask(ctx.WithFilters("linked"), as, astral.GenerateIdentity(), "route.outcomes")

	return facts, handlerGone(ctx, facts, s.Nodes, admin1, app.Identity, caller, as)
}

// handlerGone serves a provider on node2 that advertises a service, ends its
// handler while its advertisement stays, and records what a query to it reads.
func handlerGone(ctx *astral.Context, facts map[string]any, nodes map[string]node, admin1 *astrald.Client, appID *astral.Identity, caller *astrald.Client, as asker) error {
	n2 := nodes["node2"]
	admin2 := connect(n2.Endpoint, n2.Token)
	provider, err := contracts.Register(ctx, admin2, n2.Endpoint, &auth.Permit{Action: "mod.auth.serve_apps_action"})
	if err != nil {
		return fmt.Errorf("registering the provider on node2: %w", err)
	}
	providerCli := connect(n2.Endpoint, string(provider.Token))

	// the advertisement, on its own session, outlives the handler
	binding, err := servicescli.New(nil, providerCli).Advertise(ctx, []string{service},
		func(*astral.Context, *astral.Identity, string) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		})
	if err != nil {
		return fmt.Errorf("advertising %v: %w", service, err)
	}
	defer binding.Close()

	stop, err := serve(ctx, n2, string(provider.Token))
	if err != nil {
		return fmt.Errorf("the provider could not serve on node2: %w", err)
	}
	if err := grantDiscovery(ctx, caller); err != nil {
		return fmt.Errorf("granting discovery: %w", err)
	}
	facts["provider_listed"] = listed(ctx, caller, provider.Identity)
	facts["node1_knows_the_provider"] = waitHost(ctx, admin1, provider.Identity, n2.Identity)
	facts["provider_served"] = ask(ctx, as, provider.Identity, "echo")

	stop()
	time.Sleep(2 * time.Second) // the node drops the handler when its bind session ends
	facts["handler_gone"] = ask(ctx, as, provider.Identity, "echo")
	facts["provider_still_listed"] = listed(ctx, caller, provider.Identity)

	// node2 takes back the caller's relay contract, so it refuses to relay
	// for the caller: a relay's refusal, not a missing route
	facts["caller_relay_withdrawn"] = withdrawRelay(ctx, admin2, appID) == nil
	facts["relay_refused"] = ask(ctx, as, provider.Identity, "echo")
	return nil
}

// withdrawRelay takes back app's relay contract on the admin's node.
func withdrawRelay(ctx *astral.Context, admin *astrald.Client, app *astral.Identity) error {
	ch, err := admin.QueryChannel(ctx, "apphost.withdraw_relay", query.Args{"identity": app.String()})
	if err != nil {
		return err
	}
	defer ch.Close()
	return ch.Switch(channel.ExpectAck, channel.PassErrors)
}

// asker is the calling app's session on its node.
type asker struct{ endpoint, token string }

// ask queries target:op as the caller and records how the query ended. The
// time is the node's answer to the route request alone: the session is
// connected and authenticated before the clock starts.
func ask(ctx *astral.Context, as asker, target *astral.Identity, op string) outcome {
	host, err := apphost.Connect(ctx, as.endpoint)
	if err == nil {
		err = host.AuthToken(as.token)
	}
	if err != nil {
		return outcome{Kind: "other", Error: "connecting the caller: " + err.Error()}
	}
	defer host.Close()

	q := astral.Launch(astral.NewQuery(nil, target, op))
	start := time.Now()
	conn, err := host.RouteQuery(q, astral.ZoneAll, ctx.Filters())
	o := outcome{Millis: float64(time.Since(start).Microseconds()) / 1000, Retryable: apphost.IsRetryable(err)}
	if err == nil {
		conn.Close()
		o.Kind = "accepted"
		return o
	}
	o.Error = err.Error()
	var rejected *astral.ErrRejected
	var msg *apphostapi.ErrorMsg
	switch {
	case errors.As(err, &rejected):
		o.Kind, o.Code = "rejected", int(rejected.Code)
	case errors.As(err, &msg):
		o.Kind = string(msg.Code)
	default:
		o.Kind = "other"
	}
	return o
}

// Provider is the provider app's API.
type Provider struct{}

// Echo accepts the query, answers one value and holds the query until the
// caller closes it.
//
// note: an op that closes at once after accepting can race its own accept
// over a link (the session's reset may precede the response), which this test
// does not judge.
func (*Provider) Echo(_ *astral.Context, q *routing.IncomingQuery) error {
	ch := q.Accept()
	defer ch.Close()
	if err := ch.Send(astral.NewString8("echo")); err != nil {
		return err
	}
	_, err := ch.Receive()
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// serve runs the provider API as the app that token authenticates and returns
// once the node has attached its handler.
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

// grantDiscovery has the app that app names request discovery of the test's
// service on any node, and waits for its contract.
func grantDiscovery(ctx *astral.Context, app *astrald.Client) error {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{service}}}}
	if err := cs.Append(scope); err != nil {
		return err
	}
	_, err := contracts.Request(ctx, app, &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs})
	return err
}

// listed reports whether a swarm discovery from the caller shows provider,
// within 30 seconds.
func listed(ctx *astral.Context, caller *astrald.Client, provider *astral.Identity) bool {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := servicescli.New(nil, caller).DiscoverIn(ctx, services.ReachSwarm, []string{service}, false)
		if err != nil {
			return false
		}
		for ev := range events {
			if ev.Update != nil && ev.Update.ProviderID.IsEqual(provider) && bool(ev.Update.Available) {
				return true
			}
		}
		time.Sleep(time.Second)
	}
	return false
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
