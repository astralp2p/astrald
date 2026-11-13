// Command driver is the app side of services-node-restart.
//
//	driver provide             a long-lived provider app on node2 (started by tests/lib/apps.py)
//	driver follow              a long-lived consumer app on node1 that follows the service
//	driver call <app-id> [s]   a fresh caller app calls the provider, retrying
//	driver discover [s]        a fresh app runs a one-shot swarm discovery, retrying
//
// Each reads the node it acts on from APP_NODE_ENDPOINT and APP_NODE_TOKEN (the
// node's admin token: the apps register themselves and the consumers grant
// themselves discovery). provide binds its handler at APP_HOST_IP, the node's
// host_ip. provide and follow write what they see to the JSON file
// named by ASTRAL_TESTS_APP_STATE; call and discover print one JSON line.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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
	"github.com/astralp2p/astral-go/lib/routing"
)

const (
	// service is the one name this test advertises and follows.
	service = "services-node-restart-echo"
	// op is the provider's op: the method name EchoOp's snake case.
	op = "services_node_restart_echo"
)

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: driver provide | follow | call <app-identity> [seconds] | discover [seconds]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "provide":
		err = provide()
	case "follow":
		err = follow()
	case "call":
		err = call(os.Args[2:])
	case "discover":
		err = discover(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func connect(token string) *astrald.Client {
	return astrald.New(apphost.NewRouter(os.Getenv("APP_NODE_ENDPOINT"), token))
}

// register mints an app identity on the node, with permits, and returns it with
// a client of its own token.
func register(ctx *astral.Context, permits ...*auth.Permit) (*astral.Identity, *astrald.Client, error) {
	token, err := apphostcli.New(nil, connect(os.Getenv("APP_NODE_TOKEN"))).Register(ctx, permits...)
	if err != nil {
		return nil, nil, fmt.Errorf("registering: %w", err)
	}
	return token.Identity, connect(string(token.Token)), nil
}

// grantDiscovery lets app discover service on any node, granted by the node's
// admin: a caller with no permit sees nothing of a private offering and is not
// refused, and the grant keeps the test off the swarm-app rule.
func grantDiscovery(ctx *astral.Context, app *astral.Identity) error {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{service}}}}
	if err := cs.Append(scope); err != nil {
		return err
	}
	return apphostcli.New(nil, connect(os.Getenv("APP_NODE_TOKEN"))).Grant(ctx, &apphostapi.GrantRequest{
		Identity: app,
		Permits:  []*auth.Permit{{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}},
	})
}

// registerConsumer is register plus the discovery grant.
func registerConsumer(ctx *astral.Context) (*astral.Identity, *astrald.Client, error) {
	id, cli, err := register(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := grantDiscovery(ctx, id); err != nil {
		return nil, nil, fmt.Errorf("granting discovery: %w", err)
	}
	return id, cli, nil
}

// stateFile rewrites its whole JSON file, by rename, after every update: the
// harness polls it and must never see half of it. The write runs on a goroutine
// of its own, coalescing updates, because it is file I/O that can stall for
// seconds on a loaded host, and an app that waited on it would answer the node
// late (a stalled answer once failed an ask's 10 s timeout).
type stateFile struct {
	mu    sync.Mutex
	path  string
	v     any
	dirty chan struct{}
}

func newStateFile(v any) (*stateFile, error) {
	path := os.Getenv("ASTRAL_TESTS_APP_STATE")
	if path == "" {
		return nil, errors.New("ASTRAL_TESTS_APP_STATE is not set")
	}
	s := &stateFile{path: path, v: v, dirty: make(chan struct{}, 1)}
	go s.writer()
	return s, nil
}

// update applies f under the lock and schedules a write of the result.
func (s *stateFile) update(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	select {
	case s.dirty <- struct{}{}:
	default:
	}
}

func (s *stateFile) writer() {
	for range s.dirty {
		s.mu.Lock()
		raw, _ := json.Marshal(s.v)
		s.mu.Unlock()
		tmp := s.path + ".tmp"
		if os.WriteFile(tmp, raw, 0o600) == nil {
			os.Rename(tmp, s.path)
		}
	}
}

func untilSignal(ctx *astral.Context) (*astral.Context, func()) {
	sctx, cancel := ctx.WithCancel()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go func() { <-sigs; cancel() }()
	return sctx, cancel
}

// providerState is what the provider reports: enough for the oracle to tell one
// process that reconnected and advertised again from one started again.
type providerState struct {
	Identity    string       `json:"identity"`
	Pid         int          `json:"pid"`
	Attaches    int          `json:"attaches"`    // registrations the node completed, the first and every reconnect
	Advertised  int          `json:"advertised"`  // advertise bindings the node acknowledged
	Evaluations []evaluation `json:"evaluations"` // asks the node sent for the offering, in order
	Disconnects []string     `json:"disconnects"` // errors the registrar reported, in order
	Served      int          `json:"served"`      // queries answered
}

// evaluation is one ask: the caller the node asked about, and the attach the
// provider held when it answered.
type evaluation struct {
	Caller string `json:"caller"`
	Attach int    `json:"attach"`
	At     int64  `json:"at"` // unix milliseconds
}

type provider struct {
	st *stateFile
	s  *providerState
}

// ServicesNodeRestartEcho answers "echo, <caller>".
func (p *provider) ServicesNodeRestartEcho(_ *astral.Context, q *routing.IncomingQuery) error {
	caller := q.Caller().String()
	ch := q.Accept()
	defer ch.Close()
	p.st.update(func() { p.s.Served++ })
	return ch.Send(astral.NewString8("echo, " + caller))
}

// provide registers a provider app, which holds ServeApps, and serves the op and
// the service until signalled. The registrar re-binds and re-attaches the
// handler when the node's session ends, and a registration hook advertises the
// service on every attach.
func provide() error {
	ctx := astral.NewContext(nil)
	id, cli, err := register(ctx, serveApps)
	if err != nil {
		return err
	}
	ps := &providerState{Identity: id.String(), Pid: os.Getpid(), Evaluations: []evaluation{}, Disconnects: []string{}}
	st, err := newStateFile(ps)
	if err != nil {
		return err
	}
	sctx, cancel := untilSignal(ctx)
	defer cancel()

	offering := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		service: func(_ *astral.Context, caller *astral.Identity) (*services.Update, error) {
			st.update(func() {
				ps.Evaluations = append(ps.Evaluations, evaluation{Caller: caller.String(), Attach: ps.Attaches, At: time.Now().UnixMilli()})
			})
			return &services.Update{Available: true}, nil
		},
	})
	defer offering.Close()

	reg := apps.NewAppRegistrar(sctx,
		apps.WithClient(apphostcli.New(nil, cli)),
		apps.WithEvents(apps.AppRegistrarEvents{OnDisconnect: func(err error) {
			st.update(func() { ps.Disconnects = append(ps.Disconnects, fmt.Sprint(err)) })
		}}))
	// why hooks: the registrar reports ready as soon as it binds, before it
	// attaches a handler; a hook runs only after the attach, on every reconnect.
	count := apps.WithRegistrationHook(func(*astral.Context) error {
		st.update(func() { ps.Attaches++ })
		return nil
	})
	advertise := apps.WithRegistrationHook(func(hctx *astral.Context) error {
		if err := offering.Serve(hctx, servicescli.New(nil, cli)); err != nil {
			return err
		}
		st.update(func() { ps.Advertised++ })
		return nil
	})
	// why APP_HOST_IP: the node dials the handler's registered endpoint for
	// every query, and only an address at the node's host_ip reaches the test
	// host from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(os.Getenv("APP_HOST_IP"), "0"))
	st.update(func() {})
	err = apps.ServeWith(sctx, routing.NewApp(&provider{st: st, s: ps}), reg, at, count, advertise)
	if sctx.Err() != nil {
		return nil
	}
	return err
}

// followerState is what the consumer's follow saw, in order, across the
// provider node's restart.
type followerState struct {
	Identity  string   `json:"identity"`
	Pid       int      `json:"pid"`
	Events    []event  `json:"events"`
	Offerings []string `json:"offerings"` // provider/name held now, sorted
	Initial   bool     `json:"initial"`   // the follow's initial outcome arrived, complete
	Err       string   `json:"err"`       // the follow ended, and why; empty while it is open
}

// event is one item of the discovery stream.
type event struct {
	Kind     string `json:"kind"`               // update | removed | initial | not_granted | error
	Provider string `json:"provider,omitempty"` // update: the provider
	Name     string `json:"name,omitempty"`
	Up       bool   `json:"up"`               // update: the offering is available
	Detail   string `json:"detail,omitempty"` // removed: the provider/name keys, comma-separated
	At       int64  `json:"at"`               // unix milliseconds
}

// follow registers a consumer app, grants it discovery, and follows the service
// with reach=swarm on its own node until signalled, recording every event.
func follow() error {
	ctx := astral.NewContext(nil)
	id, cli, err := registerConsumer(ctx)
	if err != nil {
		return err
	}
	fs := &followerState{Identity: id.String(), Pid: os.Getpid(), Events: []event{}, Offerings: []string{}}
	st, err := newStateFile(fs)
	if err != nil {
		return err
	}
	sctx, cancel := untilSignal(ctx)
	defer cancel()
	st.update(func() {})

	held := map[string]bool{}
	events, err := servicescli.New(nil, cli).DiscoverIn(sctx, services.ReachSwarm, []string{service}, true)
	if err != nil {
		return fmt.Errorf("following: %w", err)
	}
	for ev := range events {
		st.update(func() {
			e := event{At: time.Now().UnixMilli()}
			switch {
			case ev.Update != nil:
				e.Kind, e.Provider, e.Name, e.Up = "update", ev.Update.ProviderID.String(), string(ev.Update.Name), bool(ev.Update.Available)
				if e.Up {
					held[e.Provider+"/"+e.Name] = true
				} else {
					delete(held, e.Provider+"/"+e.Name)
				}
			case ev.Removed != nil:
				e.Kind = "removed"
				var keys []string
				for _, k := range ev.Removed {
					key := k.ProviderID.String() + "/" + string(k.Name)
					delete(held, key)
					keys = append(keys, key)
				}
				e.Detail = strings.Join(keys, ",")
			case ev.Initial != nil:
				e.Kind, e.Detail = "initial", fmt.Sprintf("complete=%v incomplete=%v", ev.Initial.Complete, ev.Initial.Incomplete)
				fs.Initial = ev.Initial.Complete
			case ev.NotGranted != nil:
				e.Kind, e.Detail = "not_granted", fmt.Sprint(ev.NotGranted)
			case ev.Err != nil:
				e.Kind, e.Detail = "error", ev.Err.Error()
				fs.Err = ev.Err.Error()
			}
			fs.Events = append(fs.Events, e)
			fs.Offerings = fs.Offerings[:0]
			for k := range held {
				fs.Offerings = append(fs.Offerings, k)
			}
			sort.Strings(fs.Offerings)
		})
	}
	if sctx.Err() != nil {
		return nil
	}
	return errors.New("the follow ended")
}

// budget reads the optional seconds argument.
func budget(args []string, def int) (int, error) {
	if len(args) == 0 {
		return def, nil
	}
	return strconv.Atoi(args[0])
}

// call registers a caller app and queries the provider's op until it answers or
// the budget (seconds, default 60) ends. It prints {reply, caller, attempts,
// first_error}: the error is recorded, not judged.
func call(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: driver call <app-identity> [seconds]")
	}
	secs, err := budget(args[1:], 60)
	if err != nil {
		return err
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
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
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
			return fmt.Errorf("no answer from %v within %ds; last error: %w", target, secs, err)
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

// discover registers a consumer app and runs one-shot swarm discoveries of the
// service until one finds a provider or the budget (seconds, default 60) ends.
// It prints {providers, complete, attempts}.
func discover(args []string) error {
	secs, err := budget(args, 60)
	if err != nil {
		return err
	}
	ctx := astral.NewContext(nil)
	_, cli, err := registerConsumer(ctx)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(time.Duration(secs) * time.Second)
	attempts := 0
	for {
		attempts++
		providers, complete, err := oneShot(ctx, cli)
		if err == nil && complete && len(providers) > 0 || time.Now().After(deadline) {
			b, _ := json.Marshal(map[string]any{"providers": providers, "complete": complete, "attempts": attempts})
			fmt.Println(string(b))
			if err != nil {
				return err
			}
			return nil
		}
		time.Sleep(time.Second)
	}
}

func oneShot(ctx *astral.Context, cli *astrald.Client) (providers []string, complete bool, err error) {
	providers = []string{}
	events, err := servicescli.New(nil, cli).DiscoverIn(ctx, services.ReachSwarm, []string{service}, false)
	if err != nil {
		return providers, false, err
	}
	for ev := range events {
		switch {
		case ev.Update != nil && bool(ev.Update.Available):
			providers = append(providers, ev.Update.ProviderID.String())
		case ev.Initial != nil:
			complete = ev.Initial.Complete
		case ev.Err != nil:
			err = ev.Err
		}
	}
	return providers, complete, err
}
