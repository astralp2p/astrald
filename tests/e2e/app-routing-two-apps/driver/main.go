// Command driver runs two apps on node1 that serve the same op, and a visitor
// app that calls each of them by identity. Each app must receive exactly the
// calls addressed to it; a call to an identity with no handler is refused.
//
//	driver drive  <session.json>   prints facts as JSON
//	driver verify <session.json>   the same story with fresh identities
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
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
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: driver drive|verify <session.json>")
		os.Exit(2)
	}
	facts, err := run(os.Args[2])
	var broken *brokenRouting
	if os.Args[1] == "drive" {
		// why: a routing failure is the oracle's verdict, not the driver's; the
		// driver reports it as a fact and exits 0. Only a flow that could not
		// run (a registration or a serve that failed) exits non-zero.
		out := map[string]any{}
		for k, v := range facts {
			out[k] = v
		}
		if errors.As(err, &broken) {
			out["problem"] = broken.Error()
		}
		line, _ := json.Marshal(map[string]any{"app_routing_two_apps": out})
		fmt.Println(string(line))
		if broken != nil {
			fmt.Fprintln(os.Stderr, "BROKEN:", err)
			return
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

// brokenRouting is a routing result that differs from the expected one, as
// opposed to a flow that could not run.
type brokenRouting struct{ error }

func broke(format string, a ...any) error {
	return &brokenRouting{fmt.Errorf(format, a...)}
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// seen is one query as the serving app received it.
type seen struct{ caller, target string }

// Who is an app's API: both apps serve the same op, and each answers with its
// own name. It records what the node delivered for every call.
type Who struct {
	name string
	mu   sync.Mutex
	got  []seen
}

// RtaWho answers with the app's name.
func (w *Who) RtaWho(_ *astral.Context, q *routing.IncomingQuery) error {
	w.mu.Lock()
	w.got = append(w.got, seen{q.Caller().String(), q.Target().String()})
	w.mu.Unlock()
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8(w.name))
}

func (w *Who) calls() []seen {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]seen(nil), w.got...)
}

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

	// two apps, each with its own identity, token and client, serving the same op
	alpha, bravo := &Who{name: "alpha"}, &Who{name: "bravo"}
	alphaApp, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering alpha: %w", err)
	}
	stopA, err := serve(ctx, n1.Endpoint, string(alphaApp.Token), alpha)
	if err != nil {
		return facts, fmt.Errorf("alpha could not serve: %w", err)
	}
	defer stopA()
	bravoApp, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering bravo: %w", err)
	}
	stopB, err := serve(ctx, n1.Endpoint, string(bravoApp.Token), bravo)
	if err != nil {
		return facts, fmt.Errorf("bravo could not serve: %w", err)
	}
	defer stopB()
	facts["both_serve"] = true

	// a visitor, and a third app that registers but never serves
	visitor, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the visitor: %w", err)
	}
	visitorCli := connect(n1.Endpoint, string(visitor.Token))
	idle, err := apphostcli.New(nil, admin).Register(ctx)
	if err != nil {
		return facts, fmt.Errorf("registering the idle app: %w", err)
	}

	// calls interleaved, so a router that picks the first or the last handler,
	// or alternates between them, answers some of them as the wrong app
	order := []struct {
		to   *astral.Identity
		want string
	}{
		{alphaApp.Identity, "alpha"}, {bravoApp.Identity, "bravo"}, {alphaApp.Identity, "alpha"},
		{bravoApp.Identity, "bravo"}, {alphaApp.Identity, "alpha"}, {bravoApp.Identity, "bravo"},
		{bravoApp.Identity, "bravo"}, {alphaApp.Identity, "alpha"},
	}
	wantA, wantB := 0, 0
	facts["answers_by_target"] = true
	for i, c := range order {
		got, err := who(ctx, visitorCli, c.to)
		if err != nil || got != c.want {
			facts["answers_by_target"] = false
			return facts, broke("call %d to %s answered %q (err %v); want %q", i, c.to, got, err, c.want)
		}
		if c.want == "alpha" {
			wantA++
		} else {
			wantB++
		}
	}

	// each app received exactly the calls addressed to it, as the visitor
	facts["only_own_calls"] = true
	for _, a := range []struct {
		app  *Who
		id   *astral.Identity
		want int
	}{{alpha, alphaApp.Identity, wantA}, {bravo, bravoApp.Identity, wantB}} {
		got := a.app.calls()
		if len(got) != a.want {
			facts["only_own_calls"] = false
			return facts, broke("%s received %d calls; want %d", a.app.name, len(got), a.want)
		}
		for _, g := range got {
			if g.target != a.id.String() || g.caller != visitor.Identity.String() {
				facts["only_own_calls"] = false
				return facts, broke("%s received a call for %s from %s; want for %s from %s",
					a.app.name, g.target, g.caller, a.id, visitor.Identity)
			}
		}
	}

	// an identity with no handler is refused cleanly: an app that registered
	// but never served, and an identity the node has never seen
	for _, c := range []struct {
		fact string
		to   *astral.Identity
		desc string
	}{{"idle_refused", idle.Identity, "an app that never served"}, {"unknown_refused", astral.GenerateIdentity(), "an unknown identity"}} {
		_, err := who(ctx, visitorCli, c.to)
		facts[c.fact] = err != nil && err.Error() == apphostapi.ErrCodeRouteNotFound
		if !facts[c.fact] {
			return facts, broke("calling %s returned err %v; want route not found", c.desc, err)
		}
	}

	// the refused calls reached neither app
	facts["refusals_reach_nobody"] = len(alpha.calls()) == wantA && len(bravo.calls()) == wantB
	if !facts["refusals_reach_nobody"] {
		return facts, broke("refused calls reached an app: alpha %d (want %d), bravo %d (want %d)",
			len(alpha.calls()), wantA, len(bravo.calls()), wantB)
	}

	fmt.Fprintln(os.Stderr, "ok: alpha and bravo each received only their own calls; calls to unserved identities were refused")
	return facts, nil
}

// serve runs the API as the app that token authenticates, through its own
// client and registrar. It returns once the node has attached the app's
// handler, or with the error that stopped the registration.
//
// why a registration hook: the registrar reports ready as soon as it binds,
// before it attaches a handler; a hook runs only after the attach.
func serve(ctx *astral.Context, endpoint, token string, api *Who) (func(), error) {
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
	go func() { served <- apps.ServeWith(sctx, routing.NewApp(api), reg, onAttach) }()

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

// who queries app:rta_who as the client's app and returns the reply.
func who(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := caller.WithTarget(app).QueryChannel(cctx, "rta_who", nil)
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
