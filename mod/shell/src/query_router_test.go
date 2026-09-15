package shell

import (
	"errors"
	"io"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/shell"
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

// A shell.shell query off a link is refused before it reaches the shell scope.
// The op it names hands the caller an interactive session on this node.
func TestRouteQueryRefusesShellOffALink(t *testing.T) {
	mod := testModule(t)
	spy := &spyRouter{}
	mod.scopes.Add(shell.ModuleName, spy)

	q := astral.Launch(astral.NewQuery(astral.GenerateIdentity(), mod.node.Identity(), "shell.shell"))
	q.Extra.Set("origin", astral.OriginNetwork)

	_, err := mod.RouteQuery(astral.NewContext(nil), q, nil)

	if spy.reached {
		t.Fatal("shell.shell off a link reached the shell scope")
	}
	if !errors.Is(err, &astral.ErrRejected{}) {
		t.Fatalf("shell.shell off a link resolved as %v, want ErrRejected", err)
	}
}

// A local origin reaches the scopes. The empty scope router answers
// RouteNotFound, which is the op being absent rather than the caller refused —
// an apphost guest and the node itself must keep reaching ops.
func TestRouteQueryAdmitsLocalOrigins(t *testing.T) {
	for _, origin := range []any{nil, "", astral.OriginLocal} {
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
