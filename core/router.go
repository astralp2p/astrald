package core

import (
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

var _ astral.Router = &Router{}

const routingTimeout = 60 * time.Second

// Router wraps a PriorityRouter, tracking live connections and running query preprocessors around each route.
type Router struct {
	node *Node
	*routing.PriorityRouter
	conns         sig.Map[astral.Nonce, *conn]
	preprocessors sig.Set[QueryPreprocessor]
}

// QueryPreprocessor inspects and mutates each query before routing; returning an error or blocking rejects it.
type QueryPreprocessor interface {
	PreprocessQuery(*QueryModifier) error
}

func NewRouter(node *Node) *Router {
	var router = &Router{
		node:           node,
		PriorityRouter: routing.NewPriorityRouter("core"),
	}

	return router
}

func (r *Router) AddQueryPreprocessor(f QueryPreprocessor) error {
	return r.preprocessors.Add(f)
}

// RouteQuery defaults missing caller/target to this node, runs preprocessors (any block yields RouteNotFound), then routes and logs the outcome.
func (r *Router) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (target io.WriteCloser, err error) {
	if q.Caller == nil {
		q.Caller = r.node.identity
	}
	if q.Target == nil {
		q.Target = r.node.identity
	}

	// copy query filters from the context
	q.Extra.Replace("filters", ctx.Filters())

	qm := &QueryModifier{query: q}
	// preprocess the query
	for _, p := range r.preprocessors.Clone() {
		err = p.PreprocessQuery(qm)
		if err != nil {
			r.node.log.Logv(2, "%v query blocked by %v: %v", q.Query, p, err)
			return query.RouteNotFound()
		}

		// check if the query was blocked
		if err := qm.blocked.Get(); err != nil {
			return query.RouteNotFound()
		}
	}

	// log the start of routing
	if r.node.config.LogRoutingStart {
		r.node.log.Logv(2, "%v routing...", q.Query)
	}

	var startedAt = time.Now()
	target, err = r.routeQuery(ctx, q, w)
	var d = time.Since(startedAt).Round(1 * time.Microsecond)

	// log routing results
	if err == nil {
		r.node.log.Infov(0, "%v routed in %v", q.Query, d)
	} else {
		r.node.log.Errorv(0, "%v error (%v): %v", q.Query, d, err)

	}

	return target, err
}

func (r *Router) routeQuery(ctx *astral.Context, q *astral.InFlightQuery, src io.WriteCloser) (w io.WriteCloser, err error) {
	c, ok := r.conns.Set(q.Nonce, newConn(r, q))
	if !ok {
		return query.RouteNotFound()
	}
	c.src = newWriter(c, src)
	c.mu.Lock()

	actx, cancel := ctx.WithTimeout(routingTimeout)
	defer cancel()

	w, err = r.PriorityRouter.RouteQuery(actx, q, c.src)
	if err != nil {
		r.conns.Delete(q.Nonce)
		return nil, err
	}

	c.dst = newWriter(c, w)
	c.mu.Unlock()

	return c.dst, nil
}
