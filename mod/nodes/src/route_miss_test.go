package nodes

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

// A query to an app whose relay contract has not reached this node: the
// routing-time ask fetches it from a linked member when one holds it; when no
// member holds it, RouteQuery's tail (pullRelays, then unrelayed) answers a
// missing route, never unavailable. A node identity this node holds an
// endpoint for stays unavailable.
func TestAnAppWithoutAContractAnywhereIsAMissingRoute(t *testing.T) {
	f := newPullFixture()
	f.mod.db = testEndpointDB(t)

	app, node := astral.GenerateIdentity(), astral.GenerateIdentity()
	if err := f.mod.db.AddEndpoint(node, "tcp", "203.0.113.7:1791", nil); err != nil {
		t.Fatalf("add endpoint: %v", err)
	}

	for _, tc := range []struct {
		name      string
		target    *astral.Identity
		wantCode7 bool
	}{
		{"app with no contract held anywhere", app, false},
		{"node with a stored endpoint", node, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := networkCtx()
			if relays := f.mod.pullRelays(ctx, tc.target); len(relays) != 0 {
				t.Fatalf("pullRelays gave %v; want no relay", relays)
			}
			_, err := f.mod.unrelayed(ctx, tc.target)

			var rejected *astral.ErrRejected
			isCode7 := errors.As(err, &rejected) && rejected.Code == astral.CodeUnavailable
			switch {
			case tc.wantCode7 && !isCode7:
				t.Fatalf("answered %v; want rejected %d", err, astral.CodeUnavailable)
			case !tc.wantCode7 && !errors.Is(err, &astral.ErrRouteNotFound{}):
				t.Fatalf("answered %v; want route not found", err)
			}
		})
	}
	if n := f.user.fetches.Load(); n != 2 {
		t.Errorf("asked the members %d times; want once per target", n)
	}
}
