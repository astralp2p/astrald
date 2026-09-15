package shell

import (
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

// testNode carries an identity and nothing else; RouteQuery is never reached,
// because this router either refuses or hands off to its own scopes.
type testNode struct{ id *astral.Identity }

func (n testNode) Identity() *astral.Identity { return n.id }

func (n testNode) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	return query.RouteNotFound()
}

func testModule(t *testing.T) *Module {
	t.Helper()
	return &Module{
		node:   testNode{id: astral.GenerateIdentity()},
		scopes: routing.NewScopeRouter(routing.NewOpRouter()),
	}
}

// spyRouter stands in for a mounted scope and records whether it was reached.
type spyRouter struct{ reached bool }

func (r *spyRouter) RouteQuery(*astral.Context, *astral.InFlightQuery, io.WriteCloser) (io.WriteCloser, error) {
	r.reached = true
	return query.RouteNotFound()
}

func nodeQuery(mod *Module, origin any) *astral.InFlightQuery {
	q := astral.Launch(astral.NewQuery(astral.GenerateIdentity(), mod.node.Identity(), "nodes.new_link"))
	if origin != nil {
		q.Extra.Set("origin", origin)
	}
	return q
}

// An MCP-origin query is refused before it reaches the scopes, and refused
// hard: PriorityRouter stops on ErrRejected, so the caller reads a refusal
// rather than a missing route.
func TestRouteQueryRejectsMCPOrigin(t *testing.T) {
	mod := testModule(t)

	_, err := mod.RouteQuery(astral.NewContext(nil), nodeQuery(mod, astral.OriginMCP), nil)

	if !errors.Is(err, &astral.ErrRejected{}) {
		t.Fatalf("MCP-origin query resolved as %v, want ErrRejected", err)
	}
}

// A query off a link reaches the scopes. This router is the sole mount point
// for every module's op router, so refusing the network origin here would stop
// all inter-node op traffic, not just the shell. Admission to shell.shell is
// guarded at that op instead.
func TestRouteQueryAdmitsALinkQueryToTheScopes(t *testing.T) {
	mod := testModule(t)
	spy := &spyRouter{}
	mod.scopes.Add("nodes", spy)

	q := astral.Launch(astral.NewQuery(astral.GenerateIdentity(), mod.node.Identity(), "nodes.new_link"))
	q.Extra.Set("origin", astral.OriginNetwork)

	_, err := mod.RouteQuery(astral.NewContext(nil), q, nil)

	if !spy.reached {
		t.Fatal("a link query did not reach the scopes; inter-node ops are unreachable")
	}
	if errors.Is(err, &astral.ErrRejected{}) {
		t.Fatalf("a link query was refused: %v", err)
	}
}

// Every origin but MCP reaches the scopes. The empty scope router answers
// RouteNotFound, which is the op being absent rather than the caller refused.
func TestRouteQueryAdmitsNonMCPOrigins(t *testing.T) {
	for _, origin := range []any{nil, "", astral.OriginLocal, astral.OriginNetwork} {
		mod := testModule(t)

		_, err := mod.RouteQuery(astral.NewContext(nil), nodeQuery(mod, origin), nil)

		if errors.Is(err, &astral.ErrRejected{}) {
			t.Errorf("origin %q was refused; a local caller reaches the ops", origin)
		}
	}
}

// A query aimed elsewhere is not this router's, whatever its origin.
func TestRouteQueryIgnoresOtherTargets(t *testing.T) {
	mod := testModule(t)
	q := astral.Launch(astral.NewQuery(astral.GenerateIdentity(), astral.GenerateIdentity(), "nodes.new_link"))
	q.Extra.Set("origin", astral.OriginMCP)

	_, err := mod.RouteQuery(astral.NewContext(nil), q, nil)

	if errors.Is(err, &astral.ErrRejected{}) {
		t.Fatal("refused a query this router does not serve; it must fall through")
	}
}
