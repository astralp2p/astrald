package apphost

import (
	"io"
	"net"
	"testing"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
)

// stubSys satisfies sysmod.Module with a fixed node state.
type stubSys struct {
	state   string
	manager *astral.Identity
}

func (s *stubSys) Manager() *astral.Identity        { return s.manager }
func (s *stubSys) NodeState(*astral.Context) string { return s.state }

// stateModule is an apphost module with the default config on a node in
// state.
func stateModule(t *testing.T, state string) *Module {
	t.Helper()
	m := &Module{config: defaultConfig(), node: &routeRecorder{id: astral.GenerateIdentity()}}
	m.Sys = &stubSys{state: state}
	return m
}

// TestGateAdmitsDefaults checks the default gate across states, token
// presence and targets.
func TestGateAdmitsDefaults(t *testing.T) {
	ctx := astral.NewContext(nil)
	app := astral.GenerateIdentity()

	cases := []struct {
		state     string
		tokenless bool
		toApp     bool
		query     string
		want      bool
	}{
		{user.StateFresh, true, false, "sys.register_manager", true},
		{user.StateFresh, true, false, "user.info?out=json", true},
		{user.StateFresh, true, false, "apphost.register", false},
		{user.StateFresh, true, false, "user.bootstrap", false},
		{user.StateFresh, true, false, ".spec", true},
		{user.StateFresh, true, false, ".spec?out=json", true},
		{user.StateFresh, false, false, ".spec", true},
		{user.StateFresh, true, false, "apphost.whoami", false},
		{user.StateFresh, true, false, "shell.spec", false},
		{user.StateFresh, true, true, ".spec", false},
		{user.StateFresh, false, false, "sys.register_manager", true},
		{user.StateFresh, false, false, "user.bootstrap", false},
		{user.StateFresh, false, false, "apphost.set_app_register_policy", false},
		{user.StateFresh, false, false, "apphost.bind", false},
		{user.StateFresh, false, false, "apphost.register_handler", false},
		{user.StateFresh, false, true, "app.anything", true},
		{user.StateFresh, true, true, "user.info", false},

		{user.StateUnclaimed, true, false, "apphost.register", true},
		{user.StateUnclaimed, true, false, ".spec", true},
		{user.StateUnclaimed, false, false, ".spec", true},
		{user.StateUnclaimed, true, false, "apphost.whoami", false},
		{user.StateUnclaimed, true, false, "user.info", true},
		{user.StateUnclaimed, true, false, "user.bootstrap", false},
		{user.StateUnclaimed, true, false, "sys.register_manager", false},
		{user.StateUnclaimed, false, false, "user.bootstrap", true},
		{user.StateUnclaimed, false, false, "user.join", true},
		{user.StateUnclaimed, false, false, "user.derive_identity", true},
		{user.StateUnclaimed, false, false, "sys.register_manager", false},
		{user.StateUnclaimed, false, false, "apphost.set_app_register_policy", false},
		{user.StateUnclaimed, false, false, "apphost.bind", true},
		{user.StateUnclaimed, false, false, "apphost.register_handler?endpoint=tcp:127.0.0.1:1", true},
		{user.StateUnclaimed, false, false, "apphost.cancel", true},
		{user.StateUnclaimed, true, false, "apphost.bind", false},
		{user.StateUnclaimed, true, false, "apphost.register_handler", false},
		{user.StateUnclaimed, false, true, "app.anything", true},

		{user.StateClaimed, true, false, "apphost.register", true},
		{user.StateClaimed, true, false, "user.info", true},
		{user.StateClaimed, true, false, "user.expel", false},
		{user.StateClaimed, true, true, "apphost.register", false},
		{user.StateClaimed, false, false, "user.expel", true},
		{user.StateClaimed, false, false, ".spec", true},
		{user.StateClaimed, true, false, ".spec", true},
		{user.StateClaimed, true, false, "apphost.whoami", false},
		{user.StateClaimed, false, true, "app.anything", true},

		// note: user.introduce runs on a claimed node with a token alone.
		{user.StateFresh, false, false, "user.introduce?identity=peer", false},
		{user.StateFresh, true, false, "user.introduce?identity=peer", false},
		{user.StateUnclaimed, false, false, "user.introduce?identity=peer", false},
		{user.StateUnclaimed, true, false, "user.introduce?identity=peer", false},
		{user.StateClaimed, false, false, "user.introduce?identity=peer", true},
		{user.StateClaimed, true, false, "user.introduce?identity=peer", false},
	}

	for _, c := range cases {
		q := &astral.Query{QueryString: astral.String32(c.query)}
		if c.toApp {
			q.Target = app
		}
		if got := stateModule(t, c.state).gateAdmits(ctx, q, c.tokenless); got != c.want {
			t.Errorf("%v, tokenless %v, to app %v, %v: admits %v; want %v",
				c.state, c.tokenless, c.toApp, c.query, got, c.want)
		}
	}
}

// TestGateAdmitsTheNodeTarget: a query naming the node as target is a query
// to the node.
func TestGateAdmitsTheNodeTarget(t *testing.T) {
	node := &routeRecorder{id: astral.GenerateIdentity()}
	m := stateModule(t, user.StateFresh)
	m.node = node

	q := &astral.Query{Target: node.id, QueryString: "user.expel"}
	if m.gateAdmits(astral.NewContext(nil), q, false) {
		t.Fatal("fresh node admitted user.expel addressed to the node")
	}
}

// TestGateWildcard: "*" matches every op only as a whole entry.
func TestGateWildcard(t *testing.T) {
	ctx := astral.NewContext(nil)
	m := stateModule(t, user.StateFresh)
	m.config.Gate = Gate{
		Tokenless: []string{"user.*"},
		States:    GateStates{Fresh: []string{gateAny}},
	}

	if !m.gateAdmits(ctx, &astral.Query{QueryString: "user.expel"}, false) {
		t.Error(`fresh "*" refused a token query`)
	}
	if m.gateAdmits(ctx, &astral.Query{QueryString: "user.info"}, true) {
		t.Error(`partial pattern "user.*" admitted user.info`)
	}
}

// callerNode records the caller of each query routed through it and routes
// none.
type callerNode struct {
	astral.Node
	id     *astral.Identity
	caller chan *astral.Identity
}

func (n *callerNode) Identity() *astral.Identity { return n.id }

func (n *callerNode) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, _ io.WriteCloser) (io.WriteCloser, error) {
	n.caller <- q.Caller
	return query.RouteNotFound()
}

// TestRouteQueryNamesTheSessionForAnEmptyCaller: core.Router names an empty
// caller the node, so a session with a token that leaves the caller empty
// reaches the router as its own identity.
func TestRouteQueryNamesTheSessionForAnEmptyCaller(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	defer cli.Close()
	go io.Copy(io.Discard, cli)

	node := &callerNode{id: astral.GenerateIdentity(), caller: make(chan *astral.Identity, 1)}
	mod := &Module{node: node, log: log.New(nil)}
	mod.Sys = &stubSys{state: user.StateFresh}
	mod.config.Gate.States.Fresh = []string{gateAny}
	guest := NewGuest(mod, srv)
	guest.guestID = astral.GenerateIdentity()

	err := guest.onRouteQueryMsg(astral.NewContext(nil), &apphost.RouteQueryMsg{
		Nonce: astral.NewNonce(),
		Query: "user.expel",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	if got := <-node.caller; !got.IsEqual(guest.guestID) {
		t.Fatalf("caller = %v; want the session's identity %v", got, guest.guestID)
	}
}
