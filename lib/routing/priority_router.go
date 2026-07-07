package routing

import (
	"cmp"
	"errors"
	"io"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/query"
)

var _ astral.Router = &PriorityRouter{}

// PriorityRouter tries registered routers in ascending priority order; the
// first successful match or hard rejection (ErrRejected) wins.
type PriorityRouter struct {
	Name    string
	entries sig.Set[*Entry]
}

type PriorityAdder interface {
	Add(r astral.Router, prio int) error
}

var _ PriorityAdder = &PriorityRouter{}

type Entry struct {
	Router astral.Router
	Prio   int
}

func NewPriorityRouter(name string) *PriorityRouter {
	return &PriorityRouter{Name: name}
}

// RouteQuery iterates entries in priority order; stops on success or
// ErrRejected, collects other errors, and falls back to ErrRouteNotFound.
func (router *PriorityRouter) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (rw io.WriteCloser, err error) {
	var errs []error

	for _, r := range router.entries.Clone() {
		rw, err = r.Router.RouteQuery(ctx, q, w)
		switch {
		case err == nil:
			return
		case errors.Is(err, &astral.ErrRejected{}):
			return
		default:
			errs = append(errs, err)
		}
	}

	return query.RouteNotFound()
}

// Add registers a router at the given priority and re-sorts the entry set so
// iteration order matches ascending priority.
func (router *PriorityRouter) Add(r astral.Router, prio int) error {
	router.entries.Add(&Entry{Router: r, Prio: prio})

	router.entries.Sort(func(a, b *Entry) int {
		return cmp.Compare(a.Prio, b.Prio)
	})

	return nil
}

func (router *PriorityRouter) String() string {
	if router.Name == "" {
		return "PriorityRouter"
	}
	return router.Name
}
