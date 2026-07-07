package routing

import (
	"errors"
	"io"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// ScopeRouter dispatches queries whose first path segment matches a registered
// scope name to that scope's router, stripping the prefix before forwarding;
// unmatched or unscoped queries fall through to the root router.
type ScopeRouter struct {
	root   astral.Router
	scopes sig.Map[string, astral.Router]
}

// HasSpec is implemented by routers that can describe their operations;
// ScopeRouter uses it to aggregate specs across scopes into a flat list.
type HasSpec interface {
	Spec() (list []OpSpec)
}

// ScopedOpRouter is implemented by routers that support adding individual ops
// under an explicit scope; an empty scope targets the router's root.
type ScopedOpRouter interface {
	AddScopedOp(scope string, name string, op *Op) error
}

type RouteChecker interface {
	HasRoute(name string) bool
}

// NewScopeRouter creates a ScopeRouter backed by root; a nil root is replaced
// with a NilRouter that hard-rejects all queries.
func NewScopeRouter(root astral.Router) *ScopeRouter {
	if root == nil {
		root = &NilRouter{}
	}
	return &ScopeRouter{
		root: root,
	}
}

// RouteQuery strips the scope prefix from the query string before forwarding
// to the matched scope's router; falls back to root when no scope matches or
// the query has no dot-separated prefix.
func (r *ScopeRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	opName, _ := query.Parse(q.QueryString)
	idx := strings.IndexByte(opName, '.')
	if idx == -1 {
		return r.root.RouteQuery(ctx, q, w)
	}
	scopeName, opName := opName[:idx], opName[idx+1:]

	scope, ok := r.scopes.Get(scopeName)
	if !ok {
		return r.root.RouteQuery(ctx, q, w)
	}

	// remove the prefix from the query
	rq := astral.Launch(&astral.Query{
		Nonce:       q.Query.Nonce,
		Caller:      q.Query.Caller,
		Target:      q.Query.Target,
		QueryString: q.QueryString[idx+1:],
	})

	rq.Extra = q.Extra

	return scope.RouteQuery(ctx, rq, w)
}

func (r *ScopeRouter) Add(scope string, router astral.Router) {
	r.scopes.Set(scope, router)
}

// AddScopedOp adds op to the root OpRouter when scope is empty, or to the
// named scope's OpRouter (creating it if absent); fails if the target router
// is not an OpRouter.
func (r *ScopeRouter) AddScopedOp(scope string, name string, op *Op) error {
	if scope == "" {
		root, ok := r.root.(*OpRouter)
		if !ok {
			return errors.New("root router is not an op router")
		}
		return root.AddOp(name, op)
	}

	router, ok := r.scopes.Get(scope)
	if !ok {
		router = NewOpRouter()
		r.scopes.Set(scope, router)
	}

	ops, ok := router.(*OpRouter)
	if !ok {
		return errors.New("scope router is not an op router")
	}
	return ops.AddOp(name, op)
}

// HasRoute checks the appropriate scope router for dot-prefixed names, or the
// root router for unscoped names; returns false if the target router does not
// implement RouteChecker.
func (r *ScopeRouter) HasRoute(name string) bool {
	idx := strings.IndexByte(name, '.')
	if idx == -1 {
		root, ok := r.root.(RouteChecker)
		return ok && root.HasRoute(name)
	}

	scopeName, opName := name[:idx], name[idx+1:]
	scope, ok := r.scopes.Get(scopeName)
	if ok {
		checker, ok := scope.(RouteChecker)
		return ok && checker.HasRoute(opName)
	}

	root, ok := r.root.(RouteChecker)
	return ok && root.HasRoute(name)
}

func (r *ScopeRouter) Remove(scope string) {
	r.scopes.Delete(scope)
}

// Spec aggregates op specs from all scopes (prefixing each name with
// "scope.") and from the root, excluding internal ops whose names start
// with ".".
func (r *ScopeRouter) Spec() (list []OpSpec) {
	for name, scope := range r.scopes.Clone() {
		r, ok := scope.(HasSpec)
		if !ok {
			continue
		}

		subList := r.Spec()
		for _, opSpec := range subList {
			opSpec.Name = name + "." + opSpec.Name
			list = append(list, opSpec)
		}
	}

	if r, ok := r.root.(HasSpec); ok {
		for _, opSpec := range r.Spec() {
			if strings.HasPrefix(opSpec.Name, ".") {
				continue
			}
			list = append(list, opSpec)
		}
	}
	return
}
