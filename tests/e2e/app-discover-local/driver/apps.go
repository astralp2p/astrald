package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
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
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// node is one node of the session view.
type node struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
	Identity string `json:"identity"`
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
}

// register mints a fresh app identity on n through the node's admin token.
func register(ctx *astral.Context, n node, permits ...*auth.Permit) (*app, error) {
	tok, err := apphostcli.New(nil, connect(n.Endpoint, n.Token)).Register(ctx, permits...)
	if err != nil {
		return nil, err
	}
	return &app{id: tok.Identity, client: connect(n.Endpoint, string(tok.Token)), n: n}, nil
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
// the echo op, which answers msg back and remembers who asked.
type echoApp struct {
	*app
	stop func()

	mu      sync.Mutex
	callers []string
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
	served := make(chan error, 1)
	go func() { served <- apps.ServeWith(sctx, ops, reg, apps.WithRegistrationHook(advertise)) }()
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
	e.mu.Lock()
	e.callers = append(e.callers, q.Caller().String())
	e.mu.Unlock()
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8(args.Msg))
}

// servedOnlyBy reports whether every echo call this app answered came from id,
// and there was at least one.
func (e *echoApp) servedOnlyBy(id *astral.Identity) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.callers {
		if c != id.String() {
			return false
		}
	}
	return len(e.callers) > 0
}

// served lists the callers of every echo call this app answered.
func (e *echoApp) served() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.callers...)
}

// discovery is a one-shot discovery read to its end.
type discovery struct {
	providers  []*astral.Identity
	notGranted []string
	outcome    *servicescli.InitialOutcome
	err        error
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
		case ev.NotGranted != nil:
			d.notGranted = ev.NotGranted
		case ev.Initial != nil:
			d.outcome = ev.Initial
		case ev.Err != nil:
			d.err = ev.Err
		}
	}
	return d
}

func (d discovery) complete() bool { return d.err == nil && d.outcome != nil && d.outcome.Complete }

// call queries provider:echo as caller with msg and returns the answer.
func call(ctx *astral.Context, caller *app, provider *astral.Identity, msg string) (string, error) {
	cctx, cancel := ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(15 * time.Second)
	defer cancel()
	ch, err := caller.client.WithTarget(provider).QueryChannel(cctx, "echo", query.Args{"msg": msg})
	if err != nil {
		return "", err
	}
	defer ch.Close()
	var answer *astral.String8
	if err := ch.Switch(channel.Expect(&answer), channel.PassErrors, channel.WithContext(cctx), channel.FailOnEOF()); err != nil {
		return "", err
	}
	return answer.String(), nil
}

func step(facts map[string]bool, name string, ok bool, format string, args ...any) error {
	facts[name] = ok
	if !ok {
		return fmt.Errorf("%s: "+format, append([]any{name}, args...)...)
	}
	fmt.Fprintln(os.Stderr, "ok:", name)
	return nil
}
