package nodes

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astrald/mod/nodes"
)

// RouteQuery routes a query to its target over a link, reusing an existing one or
// retrieving a new one. On failure it falls back to relays listed in q.Extra, sending
// caller proof first when the caller differs from the context identity. Network zone only.
func (mod *Module) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (rw io.WriteCloser, err error) {
	// check if the context allows for network queries
	if !ctx.Zone().Is(astral.ZoneNetwork) {
		return query.RouteNotFound()
	}

	// check if we're querying ourselves
	if q.Target.IsEqual(mod.node.Identity()) {
		return query.RouteNotFound()
	}

	if link := mod.linkPool.SelectLinkWith(q.Target); link != nil {
		return link.RouteQuery(ctx, q, w)
	}

	retrieveCtx, cancel := ctx.WithTimeout(120 * time.Second)
	defer cancel()

	// todo: there is error printed out  when calling identity that we cannot link with (e.g. other'session node app)
	select {
	case <-ctx.Done():
		return query.RouteNotFound()
	case result := <-mod.linkPool.RetrieveLink(retrieveCtx, q.Target, WithStrategies(nodes.StrategyBasic, nodes.StrategyTor)):
		if result.Err != nil {
			mod.log.Error("retrieve link failed: %v", result.Err)
			break
		}

		return result.Link.RouteQuery(ctx, q, w)
	}

	// try relays
	relayValue, ok := q.Extra.Get(nodes.ExtraRelayVia)
	if !ok {
		return query.RouteNotFound()
	}

	relays, ok := relayValue.([]*astral.Identity)
	if !ok {
		return query.RouteNotFound()
	}

	relayed := &relayedQuery{ctx: ctx, q: q, w: w, reach: func(relayID *astral.Identity) (astral.Router, error) {
		return mod.reachRelay(ctx, retrieveCtx, q, relayID)
	}}

	return relayed.route(relays)
}

// reachRelay returns a link to relayID for q, after pushing the caller's proof
// to it when the caller is not this node.
func (mod *Module) reachRelay(ctx, retrieveCtx *astral.Context, q *astral.InFlightQuery, relayID *astral.Identity) (astral.Router, error) {
	// never use the target as a relay to itself
	if relayID.IsEqual(q.Target) {
		return nil, errors.New("the target is not its own relay")
	}

	link := mod.linkPool.SelectLinkWith(relayID)
	if link == nil {
		result := <-mod.linkPool.RetrieveLink(retrieveCtx, relayID, WithStrategies(nodes.StrategyBasic, nodes.StrategyTor))
		if result.Err != nil {
			return nil, result.Err
		}
		link = result.Link
	}

	if !ctx.Identity().IsEqual(q.Caller) {
		if err := mod.sendCallerProof(ctx, q, relayID); err != nil {
			return nil, err
		}
	}

	return link, nil
}

// relayedQuery is one query routed through the relays its Extra lists.
type relayedQuery struct {
	ctx   *astral.Context
	q     *astral.InFlightQuery
	w     io.WriteCloser
	reach func(relayID *astral.Identity) (astral.Router, error)
}

// route asks each relay in order and answers the first that accepts. When none
// accepts, it answers the first rejection that carries a code other than the
// generic astral.DefaultRejectCode, and ErrRouteNotFound when no relay answered
// one.
//
// why a rejection does not end the loop: it answers for one relay's path, and
// a later relay may still accept.
//
// why a rejection outranks a missing route: its code tells the caller a refusal
// from an absence, and a missing route tells nothing.
//
// why the generic code counts as a missing route: a node answers a query it
// has no route for over a link with the generic code (Mux.handleInboundQuery),
// so that code does not tell a refusal from an absence.
func (r *relayedQuery) route(relays []*astral.Identity) (io.WriteCloser, error) {
	var rejected *astral.ErrRejected

	for _, relayID := range relays {
		rw, err := r.ask(relayID)
		if err == nil {
			return rw, nil
		}

		var reject *astral.ErrRejected
		if rejected == nil && errors.As(err, &reject) && reject.Code != astral.DefaultRejectCode {
			rejected = reject
		}
	}

	if rejected != nil {
		return nil, rejected
	}

	return query.RouteNotFound()
}

// ask asks one relay the query.
//
// why a missing route whatever reach answered: a relay that reach did not
// reach, or did not prove the caller to, was never asked the query, so no
// rejection is its answer.
func (r *relayedQuery) ask(relayID *astral.Identity) (io.WriteCloser, error) {
	relay, err := r.reach(relayID)
	if err != nil {
		return query.RouteNotFound()
	}

	return relay.RouteQuery(r.ctx, r.q, r.w)
}

func (mod *Module) sendCallerProof(ctx *astral.Context, q *astral.InFlightQuery, target *astral.Identity) error {
	v, ok := q.Extra.Get(nodes.ExtraCallerProof)
	if !ok {
		return errors.New("missing caller proof")
	}

	callerProof := v.(astral.Object)
	if callerProof == nil {
		return errors.New("missing caller proof")
	}

	err := mod.Objects.Push(ctx, target, callerProof)
	if err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	return nil
}
