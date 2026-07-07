package routing

import (
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/query"
)

// OpRouter dispatches incoming queries by matching the first path segment of
// the query string against a fixed map of named Op handlers.
type OpRouter struct {
	routes sig.Map[string, *Op]
}

// NewOpRouter makes a new OpRouter.
func NewOpRouter(s ...any) *OpRouter {
	r := &OpRouter{}
	for _, a := range s {
		r.AddStruct(a)
	}
	return r
}

func (router *OpRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	routeName, _ := query.Parse(q.QueryString)

	route, found := router.routes.Get(routeName)

	if !found {
		return query.RouteNotFound()
	}

	return route.RouteQuery(ctx, q, w)
}

// AddOp adds a route to the router
func (router *OpRouter) AddOp(name string, op *Op) error {
	_, ok := router.routes.Set(name, op)
	if !ok {
		return errors.New("route already exists")
	}
	return nil
}

// AddScopedOp rejects any non-empty scope; OpRouter is flat and cannot host
// scoped routes — use ScopeRouter to compose scoped hierarchies.
func (router *OpRouter) AddScopedOp(scope string, name string, op *Op) error {
	if scope != "" {
		return errors.New("op router cannot add scoped route")
	}
	return router.AddOp(name, op)
}

func (router *OpRouter) HasRoute(name string) bool {
	_, ok := router.routes.Get(name)
	return ok
}

// AddStruct adds to the set all methods of a struct that have a valid op signature
func (router *OpRouter) AddStruct(s any) (err error) {
	return router.AddStructPrefix(s, "")
}

// AddStructPrefix adds to the set all methods of a struct that start with the given prefix (the prefix is removed).
func (router *OpRouter) AddStructPrefix(s any, prefix string) (err error) {
	var errs []error
	v := reflect.ValueOf(s)

	if (v.Kind() != reflect.Pointer) || (v.Elem().Kind() != reflect.Struct) {
		return errors.New("argument must be a pointer to a struct")
	}

	for i := range v.NumMethod() {
		// skip unexported methods
		if !v.Method(i).CanInterface() {
			continue
		}

		fn := v.Method(i).Interface()

		name, hadPrefix := strings.CutPrefix(v.Type().Method(i).Name, prefix)
		if !hadPrefix {
			continue // skip methods without the prefix
		}

		name = log.ToSnakeCase(name)

		op, err := NewOp(fn)
		if err != nil {
			continue
		}

		if e := router.AddOp(name, op); e != nil {
			errs = append(errs, e)
		}
	}

	return errors.Join(errs...)
}

// RemoveOp removes a route from the router
func (router *OpRouter) RemoveOp(name string) error {
	_, ok := router.routes.Delete(name)
	if !ok {
		return errors.New("route not found")
	}
	return nil
}

// GetOp returns the op registered under the given name.
func (router *OpRouter) GetOp(name string) (*Op, error) {
	route, ok := router.routes.Get(name)
	if !ok {
		return nil, errors.New("route not found")
	}
	return route, nil
}

// Spec returns specs of all operations in the router
func (router *OpRouter) Spec() (list []OpSpec) {
	for name, op := range router.routes.Clone() {
		list = append(list, OpSpec{
			Name:       name,
			Parameters: op.ArgumentSpecs(),
		})
	}
	return
}
