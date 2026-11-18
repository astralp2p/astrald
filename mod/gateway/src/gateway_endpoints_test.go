package gateway

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	gatewaymod "github.com/astralp2p/astrald/mod/gateway"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/scheduler"
	"gopkg.in/yaml.v2"
)

const weekSeconds = 7 * 24 * 60 * 60

type storedEndpoint struct {
	identity *astral.Identity
	endpoint *nodes.EndpointWithTTL
}

// recordingNodes records every stored endpoint.
//
// why: the embedded nil interface satisfies nodesmod.Module without
// implementing it. Any method other than AddEndpoint panics.
type recordingNodes struct {
	nodesmod.Module
	mu     sync.Mutex
	stored []storedEndpoint
}

func (n *recordingNodes) AddEndpoint(identity *astral.Identity, endpoint *nodes.EndpointWithTTL) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stored = append(n.stored, storedEndpoint{identity: identity, endpoint: endpoint})
	return nil
}

func (n *recordingNodes) list() []storedEndpoint {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]storedEndpoint(nil), n.stored...)
}

// tcpExonet parses tcp addresses only, as an exonet module whose tcp parser
// registers once the scheduler is ready.
//
// why: the embedded nil interface satisfies exonetmod.Module without
// implementing it. Any method other than Parse panics.
type tcpExonet struct {
	exonetmod.Module
	ready       <-chan struct{}
	parsedEarly atomic.Bool
}

func (e *tcpExonet) Parse(network string, address string) (exonet.Endpoint, error) {
	select {
	case <-e.ready:
	default:
		e.parsedEarly.Store(true)
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	if network != "tcp" {
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return tcp.ParseEndpoint(address)
}

// gatedScheduler is ready when the test closes ready, and hands every
// scheduled task to the test instead of running it.
type gatedScheduler struct {
	ready     chan struct{}
	scheduled chan scheduler.Task
}

func (s *gatedScheduler) Ready() <-chan struct{} { return s.ready }

func (s *gatedScheduler) Schedule(task scheduler.Task, _ ...scheduler.Done) (scheduler.ScheduledTask, error) {
	s.scheduled <- task
	return nil, nil
}

// yamlAssets serves one gateway.yaml, decoded as the core assets decode it.
type yamlAssets struct {
	assets.Assets
	doc string
}

func (a yamlAssets) LoadYAML(_ string, out interface{}) error {
	return yaml.UnmarshalStrict([]byte(a.doc), out)
}

// resolveGatewayRows returns the gw rows the module resolves for an identity.
func resolveGatewayRows(t *testing.T, mod *Module, identity *astral.Identity) []*nodes.EndpointWithTTL {
	t.Helper()
	ch, err := mod.ResolveEndpoints(astral.NewContext(context.Background()), identity)
	if err != nil {
		t.Fatalf("ResolveEndpoints: %v", err)
	}
	var rows []*nodes.EndpointWithTTL
	for row := range ch {
		rows = append(rows, row)
	}
	return rows
}

// A configured gateway endpoint is parsed after the scheduler is ready and
// stored for the gateway with a 7 d TTL; an endpoint that does not parse is
// skipped, and the gateway is still scheduled.
func TestRunStoresConfiguredGatewayEndpointsAfterReady(t *testing.T) {
	node := &identityNode{id: astral.GenerateIdentity()}
	gw := astral.GenerateIdentity()
	sched := &gatedScheduler{ready: make(chan struct{}), scheduled: make(chan scheduler.Task, 1)}
	store := &recordingNodes{}
	ex := &tcpExonet{ready: sched.ready}

	mod := &Module{
		Deps: Deps{Nodes: store, Exonet: ex, Scheduler: sched},
		node: node,
		log:  log.New(node.Identity()),
		config: Config{Gateways: []GatewayEntry{{
			ID:        gw,
			Endpoints: []string{"tcp:203.0.113.7:1795", "bogus:x", "tcp:notanip", "nocolon"},
		}}},
	}

	// note: the context is never cancelled: Run's shutdown unregisters through
	// astrald.Default(), which would reach a real apphost on this host.
	go mod.Run(astral.NewContext(context.Background()))

	// why: a Run that parsed before Ready would do so within this window.
	time.Sleep(100 * time.Millisecond)
	if ex.parsedEarly.Load() {
		t.Fatal("a gateway endpoint was parsed before the scheduler was ready")
	}
	close(sched.ready)

	var task *MaintainGatewayConnectionsTask
	select {
	case scheduled := <-sched.scheduled:
		task = scheduled.(*MaintainGatewayConnectionsTask)
	case <-time.After(10 * time.Second):
		t.Fatal("no gateway task was scheduled within 10s of Ready")
	}

	if !task.GatewayID.IsEqual(gw) {
		t.Errorf("task gateway: got %v, want %v", task.GatewayID, gw)
	}
	if len(task.Endpoints) != 1 {
		t.Errorf("task endpoints: got %v, want the one valid endpoint", task.Endpoints)
	}

	stored := store.list()
	if len(stored) != 1 {
		t.Fatalf("stored endpoints: got %d, want 1: %v", len(stored), stored)
	}
	got := stored[0]
	if !got.identity.IsEqual(gw) {
		t.Errorf("stored identity: got %v, want %v", got.identity, gw)
	}
	if s := got.endpoint.Network() + ":" + got.endpoint.Address(); s != "tcp:203.0.113.7:1795" {
		t.Errorf("stored endpoint: got %v, want tcp:203.0.113.7:1795", s)
	}
	if got.endpoint.TTL == nil || *got.endpoint.TTL != weekSeconds {
		t.Errorf("stored TTL: got %v, want %v s", got.endpoint.TTL, weekSeconds)
	}
}

// The module advertises gw:G:self only while the task holds a registration
// with G: not before Register succeeds, and not after the pool ends.
func TestResolveEndpointsAdvertisesOnlyWhileRegistered(t *testing.T) {
	node := &identityNode{id: astral.GenerateIdentity()}
	gw := astral.GenerateIdentity()
	store := &recordingNodes{}
	mod := &Module{Deps: Deps{Nodes: store}, node: node, log: log.New(node.Identity())}
	mod.gateways.Add(gw)

	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	t.Cleanup(cancel)

	configured, err := tcp.ParseEndpoint("203.0.113.7:1795")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	task := mod.NewMaintainGatewayConnectionsTask(gw, []exonet.Endpoint{configured}, gateway.VisibilityPublic)

	registerOK := make(chan struct{})
	reregister := make(chan struct{})
	var registers atomic.Uint64
	task.register = func(ctx *astral.Context) (*gateway.Socket, error) {
		if registers.Add(1) == 1 {
			select {
			case <-registerOK:
			case <-time.After(time.Minute):
			}
			return &gateway.Socket{}, nil
		}
		close(reregister)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Minute):
			return nil, errors.New("test timeout")
		}
	}

	poolUp := make(chan struct{})
	poolEnd := make(chan struct{})
	task.holdPool = func(*astral.Context, gateway.Socket) error {
		close(poolUp)
		select {
		case <-poolEnd:
		case <-time.After(time.Minute):
		}
		return gatewaymod.ErrSocketUnreachable
	}

	done := make(chan error, 1)
	go func() { done <- task.Run(ctx) }()

	if rows := resolveGatewayRows(t, mod, node.Identity()); len(rows) != 0 {
		t.Fatalf("before registration: got %v, want no gw row", rows)
	}

	close(registerOK)
	waitClosed(t, poolUp, "the pool did not start after Register succeeded")

	rows := resolveGatewayRows(t, mod, node.Identity())
	if len(rows) != 1 {
		t.Fatalf("while registered: got %v, want one gw row", rows)
	}
	want := gateway.NewEndpoint(gw, node.Identity())
	if rows[0].Network() != "gw" || rows[0].Address() != want.Address() {
		t.Errorf("while registered: got %v:%v, want gw:%v", rows[0].Network(), rows[0].Address(), want.Address())
	}
	if rows[0].TTL == nil || *rows[0].TTL != weekSeconds {
		t.Errorf("gw row TTL: got %v, want %v s", rows[0].TTL, weekSeconds)
	}
	if other := resolveGatewayRows(t, mod, astral.GenerateIdentity()); len(other) != 0 {
		t.Errorf("another identity: got %v, want no row", other)
	}

	close(poolEnd)
	waitClosed(t, reregister, "the task did not re-register after the pool ended")

	if rows := resolveGatewayRows(t, mod, node.Identity()); len(rows) != 0 {
		t.Fatalf("after the pool ended: got %v, want no gw row", rows)
	}
	if stored := store.list(); len(stored) != 1 || !stored[0].identity.IsEqual(gw) {
		t.Errorf("re-registration: got stored %v, want the configured endpoint stored again for %v", stored, gw)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("task Run: got %v, want %v", err, context.Canceled)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the task did not stop within 10s of cancel")
	}
}

func waitClosed(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal(msg)
	}
}

// gateway.yaml lists gateways as {id, endpoints} entries; the former list of
// bare identities stops the load.
func TestLoadGatewayEntries(t *testing.T) {
	gw := astral.GenerateIdentity()

	loaded, err := Loader{}.Load(nil, yamlAssets{doc: "gateways:\n  - id: " + gw.String() + "\n    endpoints: [\"tcp:203.0.113.7:1791\"]\n"}, nil)
	if err != nil {
		t.Fatalf("Load entry form: %v", err)
	}
	entries := loaded.(*Module).config.Gateways
	if len(entries) != 1 || !entries[0].ID.IsEqual(gw) || len(entries[0].Endpoints) != 1 || entries[0].Endpoints[0] != "tcp:203.0.113.7:1791" {
		t.Errorf("entries: got %+v, want one entry for %v with tcp:203.0.113.7:1791", entries, gw)
	}

	if _, err := (Loader{}).Load(nil, yamlAssets{doc: "gateways:\n  - " + gw.String() + "\n"}, nil); err == nil {
		t.Error("Load of a bare identity list: got nil error, want a decode error")
	}
}
