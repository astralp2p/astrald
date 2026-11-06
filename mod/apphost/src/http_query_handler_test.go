package apphost

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
)

// TestHTTPQueryObeysTheStateGate: an HTTP query holds a token, and a query to
// the node outside the state list is refused with 403 before routing.
func TestHTTPQueryObeysTheStateGate(t *testing.T) {
	cases := []struct {
		state string
		path  string
		code  int
	}{
		{user.StateFresh, "/apphost.list_tokens", http.StatusForbidden},
		{user.StateUnclaimed, "/apphost.list_tokens", http.StatusForbidden},
		{user.StateFresh, "/user.info", http.StatusNotFound},
		{user.StateClaimed, "/apphost.list_tokens", http.StatusNotFound},
	}

	for _, c := range cases {
		mod, node := gateModule(t, c.state)
		rec := httptest.NewRecorder()
		NewHTTPQueryHandler(mod, astral.GenerateIdentity()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))

		if rec.Code != c.code {
			t.Errorf("%v %v: status %v; want %v", c.state, c.path, rec.Code, c.code)
		}
		op := c.path[1:]
		if reached := node.reached(op); reached != (c.code != http.StatusForbidden) {
			t.Errorf("%v %v: reached routing %v", c.state, c.path, reached)
		}
	}
}

// opNode is a node whose router is one op.
type opNode struct {
	astral.Node
	id *astral.Identity
	op *routing.Op
}

func (n opNode) Identity() *astral.Identity { return n.id }

func (n opNode) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return n.op.RouteQuery(ctx, q, w)
}

// overTCP marks request as accepted by a TCP listener at 10.0.0.2:8624 and sent
// from remoteAddr, the way net/http marks a request it read from a TCP
// connection.
func overTCP(request *http.Request, remoteAddr string) *http.Request {
	request.RemoteAddr = remoteAddr
	local := &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 8624}
	return request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, net.Addr(local)))
}

// TestHTTPQueryFromAnotherHostIsNetwork: a query from a non-loopback address
// carries network origin, so an op that refuses network origin refuses it
// before it asks the authority. The same query from 127.0.0.1 is admitted.
func TestHTTPQueryFromAnotherHostIsNetwork(t *testing.T) {
	refusal := astral.NewErrOriginNotAllowed(astral.OriginNetwork).Error()

	cases := []struct {
		remote  string
		refused bool
		asked   int
	}{
		{"10.0.0.1:50000", true, 0},
		{"127.0.0.1:50000", false, 1},
	}

	for _, c := range cases {
		mod := testTokenModule(t)
		authority := &recordingAuth{verdict: true}
		mod.Auth = authority
		mod.config.Gate = openGate
		mod.log = log.New(nil)
		mod.Sys = &stubSys{state: user.StateClaimed}

		op, err := routing.NewOp(mod.OpListTokens)
		if err != nil {
			t.Fatal(err)
		}
		mod.node = opNode{id: astral.GenerateIdentity(), op: op}

		owner := astral.GenerateIdentity()
		if _, err := mod.CreateAccessToken(owner, astral.Duration(time.Hour)); err != nil {
			t.Fatalf("create token: %v", err)
		}

		request := httptest.NewRequest(http.MethodGet, "/apphost.list_tokens", nil)
		request.Header.Set("Accept", "application/json")
		rec := httptest.NewRecorder()
		NewHTTPQueryHandler(mod, astral.GenerateIdentity()).ServeHTTP(rec, overTCP(request, c.remote))

		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Errorf("%v: status %v; want %v", c.remote, rec.Code, http.StatusOK)
		}
		if got := strings.Contains(body, refusal); got != c.refused {
			t.Errorf("%v: answered %q; refused %v, want %v", c.remote, body, got, c.refused)
		}
		if !c.refused && (!strings.Contains(body, owner.String()) || !strings.Contains(body, "eos")) {
			t.Errorf("%v: answered %q; want the token of %v and eos", c.remote, body, owner)
		}
		if got := len(authority.recorded()); got != c.asked {
			t.Errorf("%v: asked the authority %v times; want %v", c.remote, got, c.asked)
		}
	}
}

// TestIsRemote: only a request that came over TCP from a non-loopback address
// is remote.
func TestIsRemote(t *testing.T) {
	cases := []struct {
		name   string
		local  net.Addr
		remote string
		want   bool
	}{
		{"tcp other host", &net.TCPAddr{}, "10.0.0.1:50000", true},
		{"tcp other host v6", &net.TCPAddr{}, "[2001:db8::1]:50000", true},
		{"tcp unparsable", &net.TCPAddr{}, "", true},
		{"tcp loopback", &net.TCPAddr{}, "127.0.0.1:50000", false},
		{"tcp loopback v6", &net.TCPAddr{}, "[::1]:50000", false},
		{"unix socket", &net.UnixAddr{Name: "apphost.sock", Net: "unix"}, "", false},
		{"no local address", nil, "10.0.0.1:50000", false},
	}

	for _, c := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = c.remote
		if c.local != nil {
			request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, c.local))
		}

		if got := isRemote(request); got != c.want {
			t.Errorf("%v: isRemote = %v; want %v", c.name, got, c.want)
		}
	}
}
