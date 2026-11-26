package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/astralp2p/astrald/tests/e2e/internal/contracts"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

// app is a registered app identity with a client of its own.
type app struct {
	id     *astral.Identity
	client *astrald.Client
	n      node
}

// register mints a fresh app identity on n through the node's admin token.
//
// note: skip_sibling_push holds on every node of this world, so the app's
// relay contract stays on n.
func register(ctx *astral.Context, n node, permits ...*auth.Permit) (*app, error) {
	tok, err := contracts.Register(ctx, admin(n), n.Endpoint, permits...)
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
	_, err := contracts.Request(ctx, a.client, &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: cs})
	return err
}

// echoApp is a running provider app: it advertises one service and serves
// the echo op, which answers msg back and records who asked.
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
	ops, err := e.ops()
	if err != nil {
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
	case <-time.After(converge):
		e.stop()
		return nil, fmt.Errorf("serving %v: not advertised within %v", a.id, converge)
	}
}

// ops returns the app's API: the echo op alone.
func (e *echoApp) ops() (*routing.OpRouter, error) {
	ops := routing.NewOpRouter()
	op, err := routing.NewOp(e.echo)
	if err != nil {
		return nil, err
	}
	if err := ops.AddOp("echo", op); err != nil {
		return nil, err
	}
	return ops, nil
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

// served lists the callers of every echo call this app answered.
func (e *echoApp) served() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.callers...)
}

// echo queries provider:echo once as caller with msg and returns the answer.
func echo(ctx *astral.Context, caller *app, provider *astral.Identity, msg string) (string, error) {
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

// hostsOf asks apphost.hosts on admin's node for the nodes that host app.
func hostsOf(ctx *astral.Context, c *astrald.Client, app string) ([]string, error) {
	out, errp := apphostcli.New(nil, c).Hosts(ctx, app)
	var hosts []string
	for h := range out {
		hosts = append(hosts, h.String())
	}
	return hosts, *errp
}
