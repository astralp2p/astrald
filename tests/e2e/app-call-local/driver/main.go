// Command driver runs two apps on node1, each on its own client and token: a
// greeter that serves hello, and a visitor that calls it by the greeter's app
// identity. An app whose serve_apps grant is revoked cannot serve.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
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
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	if os.Args[1] == "drive" {
		out, _ := json.Marshal(map[string]any{"app_call_local": facts})
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
	n1 := s.Nodes["node1"]
	ctx := astral.NewContext(nil)
	admin := connect(n1.Endpoint, n1.Token)

	// the greeter app: its own identity, token and client, serving hello
	greeter, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the greeter: %w", err)
	}
	stop, err := serve(ctx, n1.Endpoint, string(greeter.Token))
	facts["greeter_serves"] = err == nil
	if err != nil {
		return facts, fmt.Errorf("the greeter could not serve: %w", err)
	}
	defer stop()

	// the visitor app: another identity, token and client on the same node
	visitor, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the visitor: %w", err)
	}
	visitorCli := connect(n1.Endpoint, string(visitor.Token))

	reply, err := hello(ctx, visitorCli, greeter.Identity)
	want := "hello, " + visitor.Identity.String()
	facts["visitor_greeted"] = err == nil && reply == want
	if !facts["visitor_greeted"] {
		return facts, fmt.Errorf("the visitor called greeter:hello and got %q (err %v); want %q", reply, err, want)
	}

	// an app without serve_apps cannot serve, so a call to it reaches nobody
	stray, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the stray app: %w", err)
	}
	if err := apphostcli.New(nil, admin).RevokeAction(ctx, stray.Identity, auth.ServeAppsAction{}.ObjectType()); err != nil {
		return facts, fmt.Errorf("revoking serve_apps from the stray app: %w", err)
	}
	stopStray, err := serve(ctx, n1.Endpoint, string(stray.Token))
	if err == nil {
		stopStray()
	}
	facts["stray_refused"] = err != nil && errors.Is(err, auth.ErrNotPermitted)
	if !facts["stray_refused"] {
		return facts, fmt.Errorf("the stray app without serve_apps served: err %v; want %v", err, auth.ErrNotPermitted)
	}
	_, err = hello(ctx, visitorCli, stray.Identity)
	facts["stray_unreachable"] = err != nil && err.Error() == apphostapi.ErrCodeRouteNotFound
	if !facts["stray_unreachable"] {
		return facts, fmt.Errorf("calling the stray app returned err %v; want route not found", err)
	}

	fmt.Fprintln(os.Stderr, "ok: the visitor called the greeter on node1 and was greeted as itself")
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
func serve(ctx *astral.Context, endpoint, token string) (func(), error) {
	cli := apphostcli.New(nil, connect(endpoint, token))
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
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(&Greeter{}), reg, onAttach) }()

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

// hello queries app:hello as the client's app and returns the reply.
func hello(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.WithTimeout(10 * time.Second)
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
