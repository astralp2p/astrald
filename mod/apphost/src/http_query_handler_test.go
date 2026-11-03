package apphost

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
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
