package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// gatewayAddr is the gateway's tcp endpoint, the one its client's config names.
const gatewayAddr = "198.51.100.1:1795"

// linkingNodes is the client's nodes module: it records stored endpoints and
// hands every inbound link to the test.
type linkingNodes struct {
	recordingNodes
	links chan exonetmod.Conn
}

func (n *linkingNodes) EstablishInboundLink(_ context.Context, conn exonetmod.Conn) error {
	n.links <- conn
	return nil
}

// hasEndpoint reports whether an endpoint is stored for identity, the route a
// query to identity needs.
func (n *linkingNodes) hasEndpoint(identity *astral.Identity) bool {
	for _, s := range n.list() {
		if s.identity.IsEqual(identity) {
			return true
		}
	}
	return false
}

// gatewayHost is the gateway's tcp listener as the client's exonet reaches it:
// tcp parses, and a dial is an in-memory socket the gateway's handleInbound serves.
// Stopping it closes every socket and refuses every later dial.
type gatewayHost struct {
	exonetmod.Module
	gw *Module

	mu      sync.Mutex
	stopped bool
	served  []net.Conn
}

func (h *gatewayHost) Parse(network string, address string) (exonet.Endpoint, error) {
	if network != "tcp" {
		return nil, exonetmod.ErrUnsupportedNetwork
	}
	return tcp.ParseEndpoint(address)
}

func (h *gatewayHost) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (exonetmod.Conn, error) {
	if endpoint.Address() != gatewayAddr {
		return nil, errors.New("no route to " + endpoint.Address())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return nil, errors.New("connection refused")
	}
	clientEnd, gatewayEnd := net.Pipe()
	h.served = append(h.served, gatewayEnd)
	go h.gw.handleInbound(ctx, pipeConn{Conn: gatewayEnd})
	return pipeConn{Conn: clientEnd, outbound: true}, nil
}

func (h *gatewayHost) stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
	for _, c := range h.served {
		_ = c.Close()
	}
}

func (h *gatewayHost) isStopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// configuredGateway is a gateway G and a client T whose gateway.yaml names G
// with G's endpoint, and nothing else: no stored row for G, no link.
type configuredGateway struct {
	gw      *Module
	client  *Module
	host    *gatewayHost
	nodes   *linkingNodes
	task    *MaintainGatewayConnectionsTask
	tries   atomic.Int32 // registration attempts
	started time.Time
}

func newConfiguredGateway(t *testing.T, endpoints []string) *configuredGateway {
	t.Helper()
	gwID, clientID := astral.GenerateIdentity(), astral.GenerateIdentity()
	gwCtx, cancelGW := astral.NewContext(context.Background()).WithCancel()
	t.Cleanup(cancelGW)

	f := &configuredGateway{nodes: &linkingNodes{links: make(chan exonetmod.Conn, 4)}}
	f.gw = &Module{
		Deps:            Deps{Auth: &recordingAuth{verdict: true}, Dir: &namingDir{}},
		node:            &identityNode{id: gwID},
		log:             log.New(gwID),
		ctx:             gwCtx,
		config:          Config{Gateway: GatewayConfig{Enabled: true}},
		configEndpoints: map[string]exonet.Endpoint{"tcp": &tcp.Endpoint{IP: ip.IP(net.ParseIP("198.51.100.1").To4()), Port: 1795}},
	}
	f.host = &gatewayHost{gw: f.gw}

	sched := &gatedScheduler{ready: make(chan struct{}), scheduled: make(chan scheduler.Task, 1)}
	f.client = &Module{
		Deps: Deps{Nodes: f.nodes, Exonet: f.host, Scheduler: sched},
		node: &identityNode{id: clientID},
		log:  log.New(clientID),
		config: Config{
			Visibility: gateway.VisibilityPublic,
			Gateways:   []GatewayEntry{{ID: gwID, Endpoints: endpoints}},
		},
	}

	// note: the context is never cancelled: Run's shutdown unregisters through
	// astrald.Default(), which would reach a real apphost on this host.
	go f.client.Run(astral.NewContext(context.Background()))
	close(sched.ready)
	select {
	case scheduled := <-sched.scheduled:
		f.task = scheduled.(*MaintainGatewayConnectionsTask)
	case <-time.After(10 * time.Second):
		t.Fatal("no gateway task was scheduled within 10s of Ready")
	}
	f.task.register = f.register(t)
	return f
}

// register stands in for the routed gateway.node_register query: the query
// reaches G only when the client's nodes module stores an endpoint for G, and
// G answers it with its own OpNodeRegister.
func (f *configuredGateway) register(t *testing.T) func(*astral.Context) (*gateway.Socket, error) {
	op, err := routing.NewOp(f.gw.OpNodeRegister)
	if err != nil {
		t.Fatalf("new op: %v", err)
	}
	gwID, clientID := f.gw.node.Identity(), f.client.node.Identity()

	return func(ctx *astral.Context) (*gateway.Socket, error) {
		f.tries.Add(1)
		if !f.nodes.hasEndpoint(gwID) || f.host.isStopped() {
			_, err := query.RouteNotFound()
			return nil, err
		}
		w := newRecordingWriter()
		q := query.New(clientID, gwID, "gateway.node_register?visibility="+string(f.client.config.Visibility), nil)
		if _, err := op.RouteQuery(ctx, astral.Launch(q), w); err != nil {
			return nil, err
		}
		obj, err := receiveAnswer(w)
		if err != nil {
			return nil, err
		}
		socket, ok := obj.(*gateway.Socket)
		if !ok {
			return nil, errors.New("node_register answered " + obj.ObjectType())
		}
		return socket, nil
	}
}

// receiveAnswer waits for an op to close w and returns its first object.
func receiveAnswer(w *recordingWriter) (astral.Object, error) {
	select {
	case <-w.closed:
	case <-time.After(10 * time.Second):
		return nil, errors.New("the op never answered")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return channel.NewReceiver(bytes.NewReader(w.buf.Bytes())).Receive()
}

// start runs the registration task until the test ends.
func (f *configuredGateway) start(t *testing.T) {
	ctx, cancel := astral.NewContext(context.Background()).WithCancel()
	t.Cleanup(cancel)
	f.started = time.Now()
	go f.task.Run(ctx)
}

// waitRegistered waits up to limit for the client to hold its registration
// with G, the state the task logs as "bound to G", and returns when it did.
func (f *configuredGateway) waitRegistered(t *testing.T, limit time.Duration) time.Duration {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, ok := f.client.registered.Get(f.gw.node.Identity().String()); ok {
			return time.Since(f.started)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the client did not register with G within %v (%d attempts)", limit, f.tries.Load())
	return 0
}

// listed returns the identities G's gateway.node_list answers.
func (f *configuredGateway) listed(t *testing.T) []*astral.Identity {
	t.Helper()
	w := newRecordingWriter()
	if err := route(t, f.gw.OpNodeList, astral.GenerateIdentity(), "gateway.node_list", w); err != nil {
		t.Fatalf("node_list: %v", err)
	}
	answered(t, w)
	w.mu.Lock()
	defer w.mu.Unlock()
	var ids []*astral.Identity
	r := channel.NewReceiver(bytes.NewReader(w.buf.Bytes()))
	for {
		obj, err := r.Receive()
		if err != nil {
			t.Fatalf("node_list answer: %v", err)
		}
		if _, eos := obj.(*astral.EOS); eos {
			return ids
		}
		if id, ok := obj.(*astral.Identity); ok {
			ids = append(ids, id)
		}
	}
}

func contains(ids []*astral.Identity, id *astral.Identity) bool {
	for _, i := range ids {
		if i.IsEqual(id) {
			return true
		}
	}
	return false
}

// Criterion 3: a client whose gateway.yaml names G with its endpoint, and no
// stored row for G, registers on its first attempt, well inside the first
// 1 s retry window, and G's node_list lists it; without the configured
// endpoint the routed registration never reaches G.
func TestConfiguredGatewayRegistersWithoutAManualEndpoint(t *testing.T) {
	t.Run("configured endpoint", func(t *testing.T) {
		f := newConfiguredGateway(t, []string{"tcp:" + gatewayAddr})
		f.start(t)

		took := f.waitRegistered(t, 5*time.Second)
		if n := f.tries.Load(); n != 1 {
			t.Errorf("registered after %d attempts; want the first", n)
		}
		t.Logf("registered %v after the task started", took.Round(time.Millisecond))
		if !contains(f.listed(t), f.client.node.Identity()) {
			t.Error("G's node_list does not list the publicly registered client")
		}
	})

	t.Run("no configured endpoint", func(t *testing.T) {
		f := newConfiguredGateway(t, nil)
		f.start(t)

		time.Sleep(1500 * time.Millisecond)
		if _, ok := f.client.registered.Get(f.gw.node.Identity().String()); ok {
			t.Fatal("registered with G without a route to it")
		}
		if n := f.tries.Load(); n < 2 {
			t.Errorf("%d registration attempts in 1.5 s; want the first and a retry", n)
		}
	})
}

// dialThroughGateway claims the client's idle conn at G the way a peer's
// Module.Dial of gw:G:client does: gateway.node_connect, then the nonce on a
// socket to G's endpoint. It returns the peer's end of the link.
func (f *configuredGateway) dialThroughGateway(t *testing.T, peer *astral.Identity) net.Conn {
	t.Helper()
	w := newRecordingWriter()
	if err := route(t, f.gw.OpNodeConnect, peer, "gateway.node_connect?identity="+f.client.node.Identity().String(), w); err != nil {
		t.Fatalf("node_connect: %v", err)
	}
	obj, err := receiveAnswer(w)
	if err != nil {
		t.Fatalf("node_connect answer: %v", err)
	}
	socket, ok := obj.(*gateway.Socket)
	if !ok {
		t.Fatalf("node_connect answered %v; want a socket", obj.ObjectType())
	}
	if socket.Endpoint.Address() != gatewayAddr {
		t.Fatalf("node_connect socket at %v; want %v", socket.Endpoint.Address(), gatewayAddr)
	}

	peerEnd, gatewayEnd := net.Pipe()
	t.Cleanup(func() { peerEnd.Close(); gatewayEnd.Close() })
	go f.gw.handleInbound(context.Background(), pipeConn{Conn: gatewayEnd})
	if err := channel.New(peerEnd).Send(&socket.Nonce); err != nil {
		t.Fatalf("send nonce: %v", err)
	}
	return peerEnd
}

// waitIdleConns waits until G holds an idle conn from the client's pool.
func (f *configuredGateway) waitIdleConns(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if node, ok := f.gw.registeredNodeByIdentity(f.client.node.Identity()); ok && len(node.idleConns.Clone()) > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the client's pool put no idle conn at G within 10s")
}

// Criterion 4, rule (a): the registering client holds no manual row for G,
// and a peer that reaches G links to the client over gw:G:client.
func TestConfiguredGatewayCarriesALinkFromAPeer(t *testing.T) {
	f := newConfiguredGateway(t, []string{"tcp:" + gatewayAddr})
	f.start(t)
	f.waitRegistered(t, 5*time.Second)
	f.waitIdleConns(t)

	peerEnd := f.dialThroughGateway(t, astral.GenerateIdentity())

	var link exonetmod.Conn
	select {
	case link = <-f.nodes.links:
	case <-time.After(10 * time.Second):
		t.Fatal("the client got no inbound link through G within 10s")
	}
	want := gateway.NewEndpoint(f.gw.node.Identity(), f.client.node.Identity())
	if link.RemoteEndpoint().Network() != NetworkName || link.RemoteEndpoint().Address() != want.Address() {
		t.Errorf("link remote endpoint %v:%v; want gw:%v", link.RemoteEndpoint().Network(), link.RemoteEndpoint().Address(), want.Address())
	}

	for _, dir := range []struct {
		name     string
		from, to io.ReadWriter
	}{{"peer to client", peerEnd, link}, {"client to peer", link, peerEnd}} {
		msg := []byte(dir.name)
		go func() { _, _ = dir.from.Write(msg) }()
		got := make([]byte, len(msg))
		if _, err := io.ReadFull(dir.to, got); err != nil || !bytes.Equal(got, msg) {
			t.Errorf("%s: read %q, %v; want %q", dir.name, got, err, msg)
		}
	}
}

// Criterion 5: once G stops, the client's pool fails, the registration ends,
// and the client stops resolving gw:G:client.
func TestConfiguredGatewayStopsAdvertisingAStoppedGateway(t *testing.T) {
	f := newConfiguredGateway(t, []string{"tcp:" + gatewayAddr})
	f.start(t)
	f.waitRegistered(t, 5*time.Second)
	f.waitIdleConns(t)

	self := f.client.node.Identity()
	if rows := resolveGatewayRows(t, f.client, self); len(rows) != 1 || rows[0].Network() != NetworkName {
		t.Fatalf("while registered: got %v, want one gw row", rows)
	}

	f.host.stop()
	stopped := time.Now()

	deadline := stopped.Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if len(resolveGatewayRows(t, f.client, self)) == 0 {
			t.Logf("gw row gone %v after G stopped", time.Since(stopped).Round(time.Millisecond))
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the client still resolves gw:G:client 20s after G stopped")
}
