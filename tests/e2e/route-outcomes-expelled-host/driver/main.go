// Command driver serves a provider app on node2, calls it from an app on
// node1, expels node2 from the swarm, and records how the next call ended.
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

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	userapi "github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

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
	out, _ := json.Marshal(map[string]any{"expelled_host": facts})
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
		Nodes map[string]node  `json:"nodes"`
		Facts map[string]any   `json:"facts"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return facts, err
	}
	n1, n2 := s.Nodes["node1"], s.Nodes["node2"]
	userToken, _ := s.Facts["user_token"].(string)
	ctx := astral.NewContext(nil)
	admin1, admin2 := connect(n1.Endpoint, n1.Token), connect(n2.Endpoint, n2.Token)

	provider, err := apphostcli.New(nil, admin2).Register(ctx, &auth.Permit{Action: "mod.auth.serve_apps_action"})
	if err != nil {
		return facts, fmt.Errorf("registering the provider on node2: %w", err)
	}
	stop, err := serve(ctx, n2, string(provider.Token))
	if err != nil {
		return facts, fmt.Errorf("the provider could not serve on node2: %w", err)
	}
	defer stop()

	app, err := apphostcli.New(nil, admin1).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the caller on node1: %w", err)
	}
	facts["node1_knows_the_provider"] = waitHost(ctx, admin1, provider.Identity, n2.Identity)
	facts["node2_knows_the_caller"] = waitHost(ctx, admin2, app.Identity, n1.Identity)
	caller := connect(n1.Endpoint, string(app.Token))
	facts["before"] = ask(ctx, caller, provider.Identity, "echo")

	facts["expelled"] = expel(ctx, connect(n1.Endpoint, userToken), n2.Identity) == nil
	time.Sleep(2 * time.Second) // the ban reaches node2 and closes the link
	facts["after"] = ask(ctx, caller, provider.Identity, "echo")
	return facts, nil
}

// expel bans node from the swarm as the User.
func expel(ctx *astral.Context, user *astrald.Client, node string) error {
	ch, err := user.QueryChannel(ctx, "user.expel", query.Args{"identity": node})
	if err != nil {
		return err
	}
	defer ch.Close()
	return ch.Switch(func(*userapi.SignedExpulsion) error { return channel.ErrBreak }, channel.PassErrors)
}

// ask queries target:op as the caller and records how the query ended.
func ask(ctx *astral.Context, caller *astrald.Client, target *astral.Identity, op string) outcome {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(90 * time.Second)
	defer cancel()
	start := time.Now()
	conn, err := caller.WithTarget(target).Query(cctx, op, nil)
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
