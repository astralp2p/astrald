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

// gateModule is an apphost module on a node that is claimed or not, with the
// allowlist of withAllowlist.
func gateModule(claimed bool) (*Module, *routeRecorder) {
	node := &routeRecorder{id: astral.GenerateIdentity()}
	mod := withAllowlist(&Module{node: node, log: log.New(nil)})
	mod.Dir = namelessDir{}
	mod.User = &stubUser{ready: closedChan()}
	if claimed {
		mod.User = &stubUser{id: astral.GenerateIdentity(), ready: closedChan()}
	}
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
// session reaches routing with an op of the active list and is refused every
// other op with denied before routing; before the claim, a WebSocket session
// from an untrusted origin is refused every op.
func TestGateConfinesTokenLessSessions(t *testing.T) {
	const untrusted = "https://evil.example"

	cases := []struct {
		name      string
		transport string
		origin    string
		claimed   bool
		op        string
		admitted  bool
	}{
		{"ipc, unclaimed, listed op", "ipc", "", false, "user.info", true},
		{"ipc, unclaimed, unlisted op", "ipc", "", false, "apphost.create_token", false},
		{"ipc, claimed, listed op", "ipc", "", true, "apphost.register", true},
		{"ipc, claimed, unclaimed-only op", "ipc", "", true, "user.info", false},
		{"ipc, claimed, unlisted op", "ipc", "", true, "messaging.create_mailbox", false},

		{"ws, no origin, unclaimed, listed op", "ws", "", false, "user.info", true},
		{"ws, trusted origin, unclaimed, listed op", "ws", trustedOrigin, false, "user.info", true},
		{"ws, trusted origin, unclaimed, unlisted op", "ws", trustedOrigin, false, "apphost.create_token", false},
		{"ws, untrusted origin, unclaimed, listed op", "ws", untrusted, false, "user.info", false},
		{"ws, untrusted origin, claimed, listed op", "ws", untrusted, true, "apphost.register", true},
		{"ws, untrusted origin, claimed, unlisted op", "ws", untrusted, true, "apphost.create_token", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mod, node := gateModule(c.claimed)

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
