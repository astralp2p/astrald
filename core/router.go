package core

import (
	"errors"
	"io"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/nodes/frames"
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

// QueryPreprocessor inspects and mutates each query before routing; returning an error blocks it.
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

// RouteQuery defaults missing caller/target to this node, runs preprocessors (a preprocessor error is returned as is), then routes and logs the outcome.
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
			r.node.log.Logv(2, "%v query blocked by %v: %v", loggedQuery(q.Query), p, err)
			// why the error as is: a policy block (astral.ErrTargetNotAllowed)
			// is final, and the caller must not read it as a missing route.
			return nil, err
		}
	}

	var logged = loggedQuery(q.Query)

	// log the start of routing
	if r.node.config.LogRoutingStart {
		r.node.log.Logv(2, "%v routing...", logged)
	}

	var startedAt = time.Now()
	target, err = r.routeQuery(ctx, q, w)
	err = clampReservedCode(err)
	var d = time.Since(startedAt).Round(1 * time.Microsecond)

	// log routing results
	if err == nil {
		r.node.log.Infov(0, "%v routed in %v", logged, d)
	} else {
		r.node.log.Errorv(0, "%v error (%v): %v", logged, d, err)

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
		c.mu.Unlock()
		return nil, err
	}

	c.dst = newWriter(c, w)
	c.mu.Unlock()

	return c.dst, nil
}

// clampReservedCode turns a target's own rejection with the link-private
// "no route here" value into the generic refusal: that value is never a reject
// code (astral-docs core-definitions/query.md).
func clampReservedCode(err error) error {
	var rejected *astral.ErrRejected
	if errors.As(err, &rejected) && rejected.Code == frames.CodeNoRoute {
		return astral.NewErrRejected(astral.DefaultRejectCode)
	}
	return err
}
