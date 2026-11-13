// Command driver is the app side of app-node-restart-reconnect.
//
//	driver serve              a long-lived provider app (started by tests/lib/apps.py)
//	driver call <app-id>      a fresh caller app calls the provider, retrying
//
// Both read the node they act on from APP_NODE_ENDPOINT and APP_NODE_TOKEN;
// serve binds its handler at APP_HOST_IP, the node's host_ip.
// serve writes what it sees to the JSON file named by ASTRAL_TESTS_APP_STATE;
// call prints one JSON line.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// op is the provider's op: the method name EchoOp's snake case.
const op = "app_node_restart_echo"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: driver serve | call <app-identity> [seconds]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "call":
		err = call(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func node() *astrald.Client {
	return astrald.New(apphost.NewRouter(os.Getenv("APP_NODE_ENDPOINT"), os.Getenv("APP_NODE_TOKEN")))
}

// register mints an app identity on the node and returns a client of its token.
func register(ctx *astral.Context) (*astral.Identity, *astrald.Client, error) {
	token, err := apphostcli.New(nil, node()).Register(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("registering: %w", err)
	}
	return token.Identity, astrald.New(apphost.NewRouter(os.Getenv("APP_NODE_ENDPOINT"), string(token.Token))), nil
}

// state is what the provider reports to the harness: enough for the oracle to
// tell one process that reconnected from a process that was started again.
type state struct {
	mu               sync.Mutex
	path             string
	Identity         string   `json:"identity"`
	Pid              int      `json:"pid"`
	Attaches         int      `json:"attaches"`           // registrations the node completed, the first and every reconnect
	Disconnects      []string `json:"disconnects"`        // errors the registrar reported, in order
	Served           int      `json:"served"`             // queries answered
	LastServedAttach int      `json:"last_served_attach"` // Attaches when the last query was answered
}

func (s *state) update(f func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
	raw, _ := json.Marshal(s)
	// why rename: the reader polls the file and must never see half of it
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		os.Rename(tmp, s.path)
	}
}

// Provider is the app's API.
type Provider struct{ st *state }

// AppNodeRestartEcho answers "echo, <caller>" and records the attach it was
// served under.
func (p *Provider) AppNodeRestartEcho(_ *astral.Context, q *routing.IncomingQuery) error {
	caller := q.Caller().String()
	ch := q.Accept()
	defer ch.Close()
	p.st.update(func() { p.st.Served++; p.st.LastServedAttach = p.st.Attaches })
	return ch.Send(astral.NewString8("echo, " + caller))
}

// serve registers a provider app on the node and serves until signalled. It
// binds once; the registrar re-binds and re-registers the handler on its own
// whenever the node's session ends.
func serve() error {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if path == "" {
		return errors.New("ASTRAL_TESTS_APP_STATE is not set")
	}
	ctx := astral.NewContext(nil)
	id, cli, err := register(ctx)
	if err != nil {
		return err
	}
	st := &state{path: path, Identity: id.String(), Pid: os.Getpid(), Disconnects: []string{}}

	sctx, cancel := ctx.WithCancel()
	defer cancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; cancel() }()

	reg := apps.NewAppRegistrar(sctx,
		apps.WithClient(apphostcli.New(nil, cli)),
		apps.WithEvents(apps.AppRegistrarEvents{OnDisconnect: func(err error) {
			st.update(func() { st.Disconnects = append(st.Disconnects, fmt.Sprint(err)) })
		}}))
	// why a hook: the registrar reports ready as soon as it binds, before it
	// attaches a handler; a hook runs only after the attach, on every reconnect.
	onAttach := apps.WithRegistrationHook(func(*astral.Context) error {
		st.update(func() { st.Attaches++ })
		return nil
	})
	// why APP_HOST_IP: the node dials the handler's registered endpoint for
	// every query, and only an address at the node's host_ip reaches the test
	// host from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(os.Getenv("APP_HOST_IP"), "0"))
	st.update(func() {})
	err = apps.ServeWith(sctx, routing.NewApp(&Provider{st: st}), reg, at, onAttach)
	if sctx.Err() != nil {
		return nil
	}
	return err
}

// call registers a caller app and queries the provider's op until it answers or
// the budget (seconds, default 60) ends. It prints {reply, caller, attempts,
// first_error}: the error is recorded, not judged.
func call(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: driver call <app-identity> [seconds]")
	}
	budget := 60
	if len(args) > 1 {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			return err
		}
		budget = n
	}
	target, err := astral.ParseIdentity(args[0])
	if err != nil {
		return fmt.Errorf("provider identity: %w", err)
	}
	ctx := astral.NewContext(nil)
	caller, cli, err := register(ctx)
	if err != nil {
		return err
	}
	out := map[string]any{"caller": caller.String(), "attempts": 0, "first_error": ""}
	deadline := time.Now().Add(time.Duration(budget) * time.Second)
	for {
		reply, err := echo(ctx, cli, target)
		out["attempts"] = out["attempts"].(int) + 1
		if err == nil {
			out["reply"] = reply
			break
		}
		if out["first_error"] == "" {
			out["first_error"] = err.Error()
		}
		if time.Now().After(deadline) {
			b, _ := json.Marshal(out)
			fmt.Println(string(b))
			return fmt.Errorf("no answer from %v within %ds; last error: %w", target, budget, err)
		}
		time.Sleep(time.Second)
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	return nil
}

// echo queries the provider's op as the client's app and returns the reply.
func echo(ctx *astral.Context, caller *astrald.Client, app *astral.Identity) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(10 * time.Second)
	defer cancel()
	ch, err := caller.WithTarget(app).QueryChannel(cctx, op, nil)
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
