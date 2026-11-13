package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/routing"
)

// node is one node of the session view.
type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
	HostIP   string `json:"host_ip"`
}

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

func loadNodes(path string) (map[string]node, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s struct {
		Nodes map[string]node `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return s.Nodes, nil
}

func connect(ep, token string) *astrald.Client { return astrald.New(apphost.NewRouter(ep, token)) }

// app is a registered app identity with a client of its own.
type app struct {
	id     *astral.Identity
	client *astrald.Client
	n      node
	token  string
}

// register mints a fresh app identity on n through the node's admin token.
func register(ctx *astral.Context, n node, permits ...*auth.Permit) (*app, error) {
	tok, err := apphostcli.New(nil, connect(n.Endpoint, n.Token)).Register(ctx, permits...)
	if err != nil {
		return nil, err
	}
	return &app{id: tok.Identity, client: connect(n.Endpoint, string(tok.Token)), n: n, token: string(tok.Token)}, nil
}

// grantDiscovery lets a discover the named service on any node, granted by
// the admin of a's node.
func grantDiscovery(ctx *astral.Context, a *app, service string) error {
	cs := astral.NewBundle()
	scope := &services.DiscoveryScope{Rules: []*services.DiscoveryRule{{Services: []astral.String8{astral.String8(service)}}}}
	if err := cs.Append(scope); err != nil {
		return err
	}
	return apphostcli.New(nil, connect(a.n.Endpoint, a.n.Token)).Grant(ctx, &apphostapi.GrantRequest{
		Identity: a.id,
		Permits:  []*auth.Permit{{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs}},
	})
}

// echoApp is a running provider app: it advertises one service and serves
// the echo op, which answers msg back.
type echoApp struct {
	*app
	stop func()
}

// startEcho serves a as an echo provider of service, through apps.ServeWith
// with a's own client, and returns once the node holds the handler and the
// advertisement.
func startEcho(ctx *astral.Context, a *app, service string) (*echoApp, error) {
	e := &echoApp{app: a}
	ops := routing.NewOpRouter()
	op, err := routing.NewOp(e.echo)
	if err != nil {
		return nil, err
	}
	if err := ops.AddOp("echo", op); err != nil {
		return nil, err
	}

	p := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		service: func(*astral.Context, *astral.Identity) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		},
	})
	advertised := make(chan struct{})
	var once sync.Once
	advertise := func(hctx *astral.Context) error {
		if err := p.Serve(hctx, servicescli.New(nil, a.client)); err != nil {
			return err
		}
		once.Do(func() { close(advertised) })
		return nil
	}

	sctx, cancel := ctx.WithCancel()
	reg := apps.NewAppRegistrar(sctx, apps.WithClient(apphostcli.New(nil, a.client)))
	// why host_ip: the node dials the handler's registered endpoint for every
	// query, and only an address at the node's host_ip reaches the test host
	// from the node's machine
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(a.n.HostIP, "0"))
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, ops, reg, at, apps.WithRegistrationHook(advertise)) }()
	e.stop = sync.OnceFunc(func() {
		cancel()
		_ = p.Close()
		<-served
	})

	select {
	case <-advertised:
		return e, nil
	case err := <-served:
		cancel()
		return nil, fmt.Errorf("serving %v: %v", a.id, err)
	case <-time.After(60 * time.Second):
		e.stop()
		return nil, fmt.Errorf("serving %v: not advertised within 60s", a.id)
	}
}

type echoArgs struct {
	Msg string
}

func (e *echoApp) echo(_ *astral.Context, q *routing.IncomingQuery, args echoArgs) error {
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8(args.Msg))
}

// discovery is a one-shot discovery read to its end.
type discovery struct {
	providers []*astral.Identity
	outcome   *servicescli.InitialOutcome
	err       error
}

func discoverOnce(ctx *astral.Context, a *app, reach services.Reach, service string) discovery {
	events, err := servicescli.New(nil, a.client).DiscoverIn(ctx, reach, []string{service}, false)
	if err != nil {
		return discovery{err: err}
	}
	var d discovery
	for ev := range events {
		switch {
		case ev.Update != nil && bool(ev.Update.Available):
			d.providers = append(d.providers, ev.Update.ProviderID)
		case ev.Initial != nil:
			d.outcome = ev.Initial
		case ev.Err != nil:
			d.err = ev.Err
		}
	}
	return d
}

func (d discovery) complete() bool { return d.err == nil && d.outcome != nil && d.outcome.Complete }

func step(facts map[string]bool, name string, ok bool, format string, args ...any) error {
	facts[name] = ok
	if !ok {
		return fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	}
	fmt.Fprintln(os.Stderr, "ok:", name)
	return nil
}

// follower is an open follow of one service, read event by event.
type follower struct {
	events <-chan servicescli.Event
	stop   func()
}

func follow(ctx *astral.Context, a *app, reach services.Reach, service string) (*follower, error) {
	fctx, cancel := ctx.WithCancel()
	events, err := servicescli.New(nil, a.client).DiscoverIn(fctx, reach, []string{service}, true)
	if err != nil {
		cancel()
		return nil, err
	}
	return &follower{events: events, stop: cancel}, nil
}

// next returns the next event of the follow, or an error when none arrives
// within 60 seconds or the follow ends.
func (f *follower) next() (servicescli.Event, error) {
	select {
	case ev, ok := <-f.events:
		if !ok {
			return ev, servicescli.ErrStreamEnded
		}
		if ev.Err != nil {
			return ev, ev.Err
		}
		return ev, nil
	case <-time.After(60 * time.Second):
		return servicescli.Event{}, fmt.Errorf("no follow event within 60s")
	}
}
