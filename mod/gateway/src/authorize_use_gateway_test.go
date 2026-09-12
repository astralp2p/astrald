package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	authmod "github.com/astralp2p/astrald/mod/auth"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// useGatewayNode is this node as node_route sees it: an identity, and a router
// that records every query the op routes onward and finds no route for it.
type useGatewayNode struct {
	id *astral.Identity

	mu     sync.Mutex
	routed []*astral.InFlightQuery
}

func (n *useGatewayNode) Identity() *astral.Identity { return n.id }

func (n *useGatewayNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, _ io.WriteCloser) (io.WriteCloser, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routed = append(n.routed, q)
	return query.RouteNotFound()
}

func (n *useGatewayNode) queries() []*astral.InFlightQuery {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]*astral.InFlightQuery(nil), n.routed...)
}

// useGatewayLinks records the inbound links node_route hands to the nodes module.
type useGatewayLinks struct {
	nodesmod.Module

	mu      sync.Mutex
	inbound []exonetmod.Conn
}

func (l *useGatewayLinks) EstablishInboundLink(_ context.Context, conn exonetmod.Conn) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.inbound = append(l.inbound, conn)
	return nil
}

func (l *useGatewayLinks) established() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.inbound)
}

// useGatewayPolicy is an auth module holding the gateway's own handler, as
// LoadDependencies registers it, and no contract or grant behind it.
type useGatewayPolicy struct {
	authmod.Module
	handler authmod.TypedHandler
}

func (a *useGatewayPolicy) Authorize(ctx *astral.Context, action auth.ActionObject) bool {
	return action.ObjectType() == a.handler.ActionType() && a.handler.Authorize(ctx, action)
}

// useGatewayConn is one end of an in-memory socket standing in for a
// registered node's idle connection.
type useGatewayConn struct{ net.Conn }

func (useGatewayConn) Outbound() bool                  { return false }
func (useGatewayConn) LocalEndpoint() exonet.Endpoint  { return nil }
func (useGatewayConn) RemoteEndpoint() exonet.Endpoint { return nil }

// useGatewayFixture is a gateway holding everything the three service ops need
// to succeed: a tcp endpoint to hand out, and one registered target with one
// idle connection. A refused op leaves all of it untouched.
type useGatewayFixture struct {
	mod    *Module
	node   *useGatewayNode
	target *astral.Identity
	idle   *idleConn
}

func newUseGatewayFixture(t *testing.T, enabled bool, authority authmod.Module) *useGatewayFixture {
	t.Helper()

	local, remote := net.Pipe()
	t.Cleanup(func() { local.Close(); remote.Close() })

	f := &useGatewayFixture{
		node:   &useGatewayNode{id: astral.GenerateIdentity()},
		target: astral.GenerateIdentity(),
	}
	f.idle = &idleConn{
		Conn:         useGatewayConn{local},
		role:         roleGateway,
		withIdentity: f.target,
		handoffCh:    make(chan struct{}),
		readyCh:      make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	f.mod = &Module{
		Deps:   Deps{Auth: authority},
		node:   f.node,
		config: Config{Gateway: GatewayConfig{Enabled: enabled}},
		configEndpoints: map[string]exonet.Endpoint{
			"tcp": &tcp.Endpoint{IP: ip.IP(net.ParseIP("198.51.100.1")), Port: 1795},
		},
	}

	registered := &registeredNode{Identity: f.target, Nonce: astral.NewNonce()}
	registered.SetVisibility(gateway.VisibilityPublic)
	registered.idleConns.Add(f.idle)
	f.mod.registeredNodes.Set(f.target.String(), registered)

	return f
}

func (f *useGatewayFixture) claimed() bool {
	select {
	case <-f.idle.handoffCh:
		return true
	default:
		return false
	}
}

// useGatewayOp is one guarded op or branch: the op and a query that reaches
// its UseGateway check.
type useGatewayOp struct {
	name  string
	op    func(*Module) any
	query func(*useGatewayFixture) string
}

func useGatewayOps() []useGatewayOp {
	return []useGatewayOp{
		{"gateway.node_register", func(m *Module) any { return m.OpNodeRegister },
			func(*useGatewayFixture) string { return "gateway.node_register?visibility=public" }},
		{"gateway.node_connect", func(m *Module) any { return m.OpNodeConnect },
			func(f *useGatewayFixture) string { return "gateway.node_connect?target=" + f.target.String() }},
		{"gateway.node_route/forward", func(m *Module) any { return m.OpNodeRoute },
			func(f *useGatewayFixture) string { return "gateway.node_route?target=" + f.target.String() }},
	}
}

// TestUseGatewayRefusesBeforeSideEffects is the coverage measure for the
// UseGateway action in mod/gateway: every service op asks before it acts, and
// a refused caller receives no bytes and changes nothing.
//
// A disabled gateway refuses even when Authorize would allow the action, and
// asks nothing: a contract or external authority does not reopen the service.
func TestUseGatewayRefusesBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name      string
		enabled   bool
		verdict   bool
		wantCalls int
	}{
		{"denied", true, false, 1},
		{"disabled", false, true, 0},
	}

	for _, c := range cases {
		for _, op := range useGatewayOps() {
			t.Run(c.name+"/"+op.name, func(t *testing.T) {
				authority := &recordingAuth{verdict: c.verdict}
				f := newUseGatewayFixture(t, c.enabled, authority)
				caller := astral.GenerateIdentity()
				w := newRecordingWriter()

				err := route(t, op.op(f.mod), caller, op.query(f), w)

				var rejected *astral.ErrRejected
				if !errors.As(err, &rejected) {
					t.Fatalf("%s answered a refused caller: got err %v, want a rejection", op.name, err)
				}
				if n := w.written(); n != 0 {
					t.Fatalf("%s wrote %d bytes to a refused caller; want none", op.name, n)
				}
				assertUseGatewayCalls(t, op.name, authority.recorded(), caller, c.wantCalls)
				assertUseGatewayUntouched(t, op.name, f, caller)
			})
		}
	}
}

func assertUseGatewayCalls(t *testing.T, name string, actions []auth.ActionObject, caller *astral.Identity, want int) {
	t.Helper()

	if len(actions) != want {
		t.Fatalf("%s made %d authorization calls; want %d", name, len(actions), want)
	}
	for _, a := range actions {
		if _, ok := a.(*auth.UseGatewayAction); !ok {
			t.Fatalf("%s named %q; want %q", name, a.ObjectType(), (&auth.UseGatewayAction{}).ObjectType())
		}
		if !a.Actor().IsEqual(caller) {
			t.Fatalf("%s named actor %v; want the caller %v", name, a.Actor(), caller)
		}
	}
}

func assertUseGatewayUntouched(t *testing.T, name string, f *useGatewayFixture, caller *astral.Identity) {
	t.Helper()

	if _, ok := f.mod.registeredNodeByIdentity(caller); ok {
		t.Fatalf("%s registered a refused caller", name)
	}
	if n := len(f.mod.connectors.Clone()); n != 0 {
		t.Fatalf("%s allocated %d connectors for a refused caller; want none", name, n)
	}
	if f.claimed() {
		t.Fatalf("%s reserved the target's idle connection for a refused caller", name)
	}
	if n := len(f.node.queries()); n != 0 {
		t.Fatalf("%s routed %d queries onward for a refused caller; want none", name, n)
	}
}

// TestUseGatewayAllowsUnrelatedCaller holds the public policy: while the
// gateway is enabled, a caller with no grant, contract, or swarm membership
// registers, reserves an idle connection, and is forwarded.
func TestUseGatewayAllowsUnrelatedCaller(t *testing.T) {
	for _, op := range useGatewayOps() {
		t.Run(op.name, func(t *testing.T) {
			f := newUseGatewayFixture(t, true, nil)
			f.mod.Auth = &useGatewayPolicy{handler: authmod.Func[*auth.UseGatewayAction](f.mod.AuthorizeUseGateway)}
			caller := astral.GenerateIdentity()
			w := newRecordingWriter()

			if err := route(t, op.op(f.mod), caller, op.query(f), w); err != nil {
				t.Fatalf("%s refused an unrelated caller on an enabled gateway: %v", op.name, err)
			}

			switch op.name {
			case "gateway.node_register":
				if _, ok := f.mod.registeredNodeByIdentity(caller); !ok || w.written() == 0 {
					t.Fatalf("%s did not register the caller and answer it", op.name)
				}
			case "gateway.node_connect":
				if !f.claimed() || len(f.mod.connectors.Clone()) != 1 || w.written() == 0 {
					t.Fatalf("%s did not reserve the idle connection and answer the caller", op.name)
				}
			default:
				assertUseGatewayForwarded(t, f)
			}
		})
	}
}

func assertUseGatewayForwarded(t *testing.T, f *useGatewayFixture) {
	t.Helper()

	routed := f.node.queries()
	if len(routed) != 1 {
		t.Fatalf("forwarding routed %d queries onward; want 1", len(routed))
	}
	q := routed[0]
	if !q.Caller.IsEqual(f.node.id) || !q.Target.IsEqual(f.target) {
		t.Fatalf("forwarding routed %v -> %v; want %v -> %v", q.Caller, q.Target, f.node.id, f.target)
	}
	if !strings.HasPrefix(q.QueryString, gateway.MethodNodeRoute) {
		t.Fatalf("forwarding routed %q; want %s", q.QueryString, gateway.MethodNodeRoute)
	}
}

// TestUseGatewayInboundRouteAsksNothing holds that node_route addressed to this
// node is link admission, not gateway use: it asks no UseGateway question and
// needs no enabled gateway.
func TestUseGatewayInboundRouteAsksNothing(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		authority := &recordingAuth{verdict: false}
		f := newUseGatewayFixture(t, enabled, authority)
		links := &useGatewayLinks{}
		f.mod.Nodes = links

		err := route(t, f.mod.OpNodeRoute, astral.GenerateIdentity(), "gateway.node_route?target="+f.node.id.String(), newRecordingWriter())
		if err != nil {
			t.Fatalf("inbound node_route (enabled=%v) was refused: %v", enabled, err)
		}
		if n := len(authority.recorded()); n != 0 {
			t.Fatalf("inbound node_route (enabled=%v) made %d authorization calls; want none", enabled, n)
		}
		if n := links.established(); n != 1 {
			t.Fatalf("inbound node_route (enabled=%v) handed %d links to the nodes module; want 1", enabled, n)
		}
	}
}

// TestUseGatewayLeavesCleanupAndListingOpen holds that node_unregister and
// node_list ask nothing and stay open on a disabled gateway, and that
// unregistering removes the caller's registration alone.
func TestUseGatewayLeavesCleanupAndListingOpen(t *testing.T) {
	authority := &recordingAuth{verdict: false}
	f := newUseGatewayFixture(t, false, authority)
	caller := astral.GenerateIdentity()
	f.mod.registeredNodes.Set(caller.String(), &registeredNode{Identity: caller, Nonce: astral.NewNonce()})

	w := newRecordingWriter()
	if err := route(t, f.mod.OpNodeUnregister, caller, "gateway.node_unregister", w); err != nil || w.written() == 0 {
		t.Fatalf("node_unregister on a disabled gateway: err %v, %d bytes", err, w.written())
	}
	if _, ok := f.mod.registeredNodeByIdentity(caller); ok {
		t.Fatalf("node_unregister kept the caller's registration")
	}
	if _, ok := f.mod.registeredNodeByIdentity(f.target); !ok {
		t.Fatalf("node_unregister removed another node's registration")
	}

	w = newRecordingWriter()
	if err := route(t, f.mod.OpNodeList, caller, "gateway.node_list", w); err != nil || w.written() == 0 {
		t.Fatalf("node_list on a disabled gateway: err %v, %d bytes", err, w.written())
	}
	if n := len(authority.recorded()); n != 0 {
		t.Fatalf("node_unregister and node_list made %d authorization calls; want none", n)
	}
}

// TestUseGatewayAuthorizerFollowsEnabled is the authorizer's policy: any actor
// while the gateway is enabled, no actor while it is disabled.
func TestUseGatewayAuthorizerFollowsEnabled(t *testing.T) {
	handler := func(enabled bool) authmod.TypedHandler {
		mod := &Module{config: Config{Gateway: GatewayConfig{Enabled: enabled}}}
		return authmod.Func[*auth.UseGatewayAction](mod.AuthorizeUseGateway)
	}

	if got, want := handler(true).ActionType(), "mod.auth.use_gateway_action"; got != want {
		t.Fatalf("handler registers for %q; want %q", got, want)
	}

	for _, enabled := range []bool{true, false} {
		for _, actor := range []*astral.Identity{astral.GenerateIdentity(), astral.GenerateIdentity()} {
			action := &auth.UseGatewayAction{Action: auth.NewAction(actor)}
			if got := handler(enabled).Authorize(astral.NewContext(nil), action); got != enabled {
				t.Fatalf("enabled=%v: authorizer answered %v for %v; want %v", enabled, got, actor, enabled)
			}
		}
	}
}
