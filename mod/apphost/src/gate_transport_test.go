package apphost

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/coder/websocket"
)

// namelessDir names no identity; every other method panics.
type namelessDir struct{ dir.Module }

func (namelessDir) DisplayName(*astral.Identity) string { return "" }

// routeRecorder is a node that records the query string of every query that
// reaches routing, and routes none.
type routeRecorder struct {
	astral.Node
	id     *astral.Identity
	mu     sync.Mutex
	routed []string
}

func (n *routeRecorder) Identity() *astral.Identity { return n.id }

func (n *routeRecorder) RouteQuery(_ *astral.Context, q *astral.InFlightQuery, _ io.WriteCloser) (io.WriteCloser, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routed = append(n.routed, q.QueryString.String())
	return query.RouteNotFound()
}

func (n *routeRecorder) reached(op string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, q := range n.routed {
		if p, _ := query.Parse(q); p == op {
			return true
		}
	}
	return false
}

// gateModule is an apphost module with the default gate on a node in state.
func gateModule(t *testing.T, state string) (*Module, *routeRecorder) {
	node := &routeRecorder{id: astral.GenerateIdentity()}
	mod := stateModule(t, state)
	mod.node = node
	mod.log = log.New(nil)
	mod.Dir = namelessDir{}
	return mod, node
}

// ask sends one token-less query for op on ch, a fresh session, and answers the
// host's reply.
func ask(t *testing.T, ch *channel.Channel, op string) astral.Object {
	t.Helper()
	if _, err := ch.Receive(); err != nil {
		t.Fatalf("host info: %v", err)
	}
	if err := ch.Send(&apphost.RouteQueryMsg{Nonce: astral.NewNonce(), Query: astral.String16(op)}); err != nil {
		t.Fatalf("send query: %v", err)
	}
	reply, err := ch.Receive()
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	return reply
}

// askOverIPC asks op on a token-less IPC session.
func askOverIPC(t *testing.T, mod *Module, op string) astral.Object {
	t.Helper()
	srv, cli := net.Pipe()
	defer cli.Close()
	go NewGuest(mod, srv).Serve(astral.NewContext(nil))
	return ask(t, channel.New(cli), op)
}

// askOverWS asks op on a token-less WebSocket session that sends origin as its
// Origin header, none when origin is empty.
func askOverWS(t *testing.T, mod *Module, origin, op string) astral.Object {
	t.Helper()
	srv := &HTTPServer{Module: mod, ctx: astral.NewContext(nil)}
	ts := httptest.NewServer(http.HandlerFunc(srv.handleWS))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{Subprotocols: []string{SubprotocolBinary}}
	if origin != "" {
		opts.HTTPHeader = http.Header{"Origin": []string{origin}}
	}
	wc, _, err := websocket.Dial(ctx, strings.Replace(ts.URL, "http://", "ws://", 1)+"/.ws", opts)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer wc.CloseNow()

	return ask(t, channel.New(websocket.NetConn(ctx, wc, websocket.MessageBinary)), op)
}

// TestGateConfinesTokenLessSessions: over IPC and WebSocket, a token-less
// session reaches routing with an op both the tokenless list and the state list
// name, and is refused every other op with denied before routing, whatever the
// WebSocket Origin.
func TestGateConfinesTokenLessSessions(t *testing.T) {
	const anyOrigin = "https://evil.example"

	cases := []struct {
		name      string
		transport string
		origin    string
		state     string
		op        string
		admitted  bool
	}{
		{"ipc, fresh, listed op", "ipc", "", user.StateFresh, "sys.register_manager", true},
		{"ipc, fresh, tokenless op of another state", "ipc", "", user.StateFresh, "apphost.register", false},
		{"ipc, fresh, unlisted op", "ipc", "", user.StateFresh, "apphost.create_token", false},
		{"ipc, unclaimed, listed op", "ipc", "", user.StateUnclaimed, "apphost.register", true},
		{"ipc, unclaimed, token-only op", "ipc", "", user.StateUnclaimed, "user.bootstrap", false},
		{"ipc, claimed, listed op", "ipc", "", user.StateClaimed, "user.info", true},
		{"ipc, claimed, unlisted op", "ipc", "", user.StateClaimed, "messaging.create_mailbox", false},

		{"ws, no origin, fresh, listed op", "ws", "", user.StateFresh, "user.info", true},
		{"ws, any origin, fresh, listed op", "ws", anyOrigin, user.StateFresh, "sys.register_manager", true},
		{"ws, any origin, fresh, unlisted op", "ws", anyOrigin, user.StateFresh, "apphost.create_token", false},
		{"ws, any origin, claimed, listed op", "ws", anyOrigin, user.StateClaimed, "apphost.register", true},
		{"ws, any origin, claimed, unlisted op", "ws", anyOrigin, user.StateClaimed, "apphost.create_token", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod, node := gateModule(t, c.state)

			var reply astral.Object
			if c.transport == "ipc" {
				reply = askOverIPC(t, mod, c.op)
			} else {
				reply = askOverWS(t, mod, c.origin, c.op)
			}

			msg, ok := reply.(*apphost.ErrorMsg)
			if !ok {
				t.Fatalf("reply %v; want an error_msg", reply)
			}
			want := apphost.ErrCodeDenied
			if c.admitted {
				want = apphost.ErrCodeRouteNotFound
			}
			if string(msg.Code) != want {
				t.Fatalf("reply %v; want %v", msg.Code, want)
			}
			if node.reached(c.op) != c.admitted {
				t.Fatalf("query reached routing: %v; want %v", node.reached(c.op), c.admitted)
			}
		})
	}
}
