package main

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	apphostcli "github.com/astralp2p/astral-go/api/apphost/client"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/services"
	servicescli "github.com/astralp2p/astral-go/api/services/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/apphost"
	"github.com/astralp2p/astral-go/lib/apps"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/tests/e2e/internal/provider"
)

// stateEvery bounds how often the state file is rewritten.
const stateEvery = time.Second

var serveApps = &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}

// serveFlags are the flags of meshapp serve.
type serveFlags struct {
	service     string
	period      time.Duration
	keepRemoved bool
	targets     []*astral.Identity
	probeNodes  []*astral.Identity
	probeOp     string
	callTimeout time.Duration
}

// helloApp answers hello and counts its callers.
type helloApp struct{ rec *recorder }

// Hello answers "hello, <caller>".
func (h *helloApp) Hello(_ *astral.Context, q *routing.IncomingQuery) error {
	caller := q.Caller().String()
	h.rec.update(func(st *appState) { st.Served[caller]++ })
	ch := q.Accept()
	defer ch.Close()
	return ch.Send(astral.NewString8("hello, " + caller))
}

// serve runs one mesh app until SIGTERM.
func serve(f serveFlags) error {
	startedAt := unixSeconds(time.Now())
	e, err := readEnv(true)
	if err != nil {
		return err
	}
	ctx, cancel := untilSignal(astral.NewContext(nil))
	defer cancel()
	app, err := registerApp(ctx, e, f.service)
	if err != nil {
		return err
	}
	nodeID, err := apphost.NewRouter(e.endpoint, e.token).HostIDContext(ctx)
	if err != nil {
		return fmt.Errorf("node identity: %w", err)
	}
	rec := newRecorder(appState{Pid: os.Getpid(), App: app.ID.String(), Node: nodeID.String(),
		Service: f.service, StartedAt: startedAt})
	log, err := openCallLog(e.statePath + ".calls.jsonl")
	if err != nil {
		return err
	}
	defer log.close()
	stop, written := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(written)
		(&stateWriter{path: e.statePath, every: stateEvery, rec: rec}).run(stop)
	}()
	m := &mesh{rec: rec, log: log, client: app.Client, self: app.ID.String(), service: f.service,
		period: f.period, keepRemoved: f.keepRemoved, probeOp: f.probeOp, callTimeout: f.callTimeout}
	err = m.run(ctx, app, f)
	close(stop)
	<-written
	return err
}

// registerApp mints the app on its node with a discovery grant for service.
func registerApp(ctx *astral.Context, e env, service string) (*provider.App, error) {
	n := provider.Node{Endpoint: e.endpoint, HostIP: e.hostIP}
	admin := provider.Connect(e.endpoint, e.token)
	app, err := provider.Register(ctx, admin, n, serveApps)
	if err != nil {
		return nil, err
	}
	scope := astral.NewBundle()
	rule := &services.DiscoveryRule{Services: []astral.String8{astral.String8(service)}}
	if err := scope.Append(&services.DiscoveryScope{Rules: []*services.DiscoveryRule{rule}}); err != nil {
		return nil, err
	}
	discover := &auth.Permit{Action: astral.String8(services.ServiceDiscoveryAction{}.ObjectType()), Constraints: scope}
	if err := provider.Grant(ctx, admin, app.ID, discover); err != nil {
		return nil, fmt.Errorf("granting discovery of %v: %w", service, err)
	}
	return app, nil
}

// run serves, advertises, follows and calls until ctx ends or serving fails.
func (m *mesh) run(ctx *astral.Context, app *provider.App, f serveFlags) error {
	ctx, cancel := ctx.WithCancel()
	defer cancel()
	var wg sync.WaitGroup
	served := make(chan error, 1)
	go func() { served <- m.serveHandler(ctx, app) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.follow(ctx, &wg)
	}()
	now := unixSeconds(time.Now())
	for _, id := range f.targets {
		if m.rec.report(kindApp, id.String(), now, true) {
			m.spawnCaller(ctx, &wg, kindApp, id)
		}
	}
	for _, id := range f.probeNodes {
		if m.rec.report(kindProbe, id.String(), now, true) {
			m.spawnCaller(ctx, &wg, kindProbe, id)
		}
	}
	var err error
	select {
	case <-ctx.Done():
	case err = <-served:
		m.rec.update(func(st *appState) { st.Fatal = fmt.Sprint(err) })
		cancel()
	}
	wg.Wait()
	return err
}

// serveHandler serves hello on a handler bound at the node's host_ip and
// advertises the service open, again after every re-registration.
func (m *mesh) serveHandler(ctx *astral.Context, app *provider.App) error {
	reg := apps.NewAppRegistrar(ctx, apps.WithClient(apphostcli.New(nil, app.Client)),
		apps.WithEvents(apps.AppRegistrarEvents{OnDisconnect: func(err error) {
			m.rec.update(func(st *appState) { st.Disconnects = append(st.Disconnects, fmt.Sprint(err)) })
		}}))
	adv := servicescli.NewProvider(map[string]servicescli.OfferingFunc{
		m.service: func(*astral.Context, *astral.Identity) (*services.Update, error) {
			return &services.Update{Available: true}, nil
		},
	})
	defer adv.Close()
	// why a hook: the registrar reports ready once it binds, before it attaches
	// the handler; a hook runs after each attach, the first and every reconnect
	hook := apps.WithRegistrationHook(func(hctx *astral.Context) error {
		m.rec.update(func(st *appState) { st.Attaches++; st.RegisteredAt = once(st.RegisteredAt) })
		if err := adv.Serve(hctx, servicescli.New(nil, app.Client)); err != nil {
			return fmt.Errorf("advertising %v: %w", m.service, err)
		}
		m.rec.update(func(st *appState) { st.AdvertisedAt = once(st.AdvertisedAt) })
		return nil
	})
	// why APP_HOST_IP: the node dials the handler's registered endpoint for
	// every query, and only an address at the node's host_ip reaches the host
	at := apps.WithHandlerAddress("tcp:" + net.JoinHostPort(app.HostIP, "0"))
	err := apps.ServeWith(ctx, routing.NewApp(&helloApp{rec: m.rec}), reg, at, hook)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// once is t unless t is set: the time a thing first happened.
func once(t *float64) *float64 {
	if t != nil {
		return t
	}
	now := unixSeconds(time.Now())
	return &now
}
