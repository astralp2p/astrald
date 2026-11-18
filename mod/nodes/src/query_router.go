package nodes

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/astralp2p/astral-go/api/objects"
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
		return nil, ctx.Err()
	case result := <-mod.linkPool.RetrieveLink(retrieveCtx, q.Target, WithStrategies(nodes.StrategyBasic, nodes.StrategyTor)):
		if result.Err != nil {
			mod.log.Error("retrieve link failed: %v", result.Err)
			break
		}

		return result.Link.RouteQuery(ctx, q, w)
	}

	// try relays
	relays, _ := q.Extra.Get(nodes.ExtraRelayVia)
	relayList, _ := relays.([]*astral.Identity)
	if len(relayList) == 0 {
		return mod.unrelayed(q.Target)
	}

	relayed := &relayedQuery{ctx: ctx, q: q, w: w, reach: func(relayID *astral.Identity) (astral.Router, error) {
		return mod.reachRelay(ctx, retrieveCtx, q, relayID)
	}}

	return relayed.route(relayList)
}

// unrelayed answers a query that no link and no relay reaches: unavailable
// when the target is a node this node holds an endpoint for, since a later
// attempt may link to it, and a missing route otherwise.
//
// why not for an identity without endpoints: a typo in a node identity, or an
// app this node holds no relay contract for, stays a missing route.
func (mod *Module) unrelayed(target *astral.Identity) (io.WriteCloser, error) {
	if mod.db.HasEndpoints(target) {
		return nil, unavailable()
	}
	return query.RouteNotFound()
}

// reachRelay returns a link to relayID for q, after pushing the caller's proof
// to it when the caller is not this node.
func (mod *Module) reachRelay(ctx, retrieveCtx *astral.Context, q *astral.InFlightQuery, relayID *astral.Identity) (astral.Router, error) {
	// never use the target as a relay to itself
	if relayID.IsEqual(q.Target) {
		return nil, errTargetIsRelay
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

// errTargetIsRelay is reach's answer for a relay that is the target itself.
var errTargetIsRelay = errors.New("the target is not its own relay")

// relayedQuery is one query routed through the relays its Extra lists.
type relayedQuery struct {
	ctx   *astral.Context
	q     *astral.InFlightQuery
	w     io.WriteCloser
	reach func(relayID *astral.Identity) (astral.Router, error)
}

// relayAnswers collects what the relays that did not accept answered.
type relayAnswers struct {
	refused     *astral.ErrRejected // the first refusal
	unavailable bool                // a relay was unreachable or answered 7
}

// route asks each relay in order and answers the first that accepts. When none
// accepts, it answers the first refusal, else unavailable when a relay was
// unavailable, else a missing route. See astral-docs topics/app-routing.md,
// Resolving the host.
//
// why a refusal does not end the loop: it answers for one relay's path, and a
// later relay may still accept.
//
// why a refusal outranks unavailable: a refusal is final, and a retry would
// meet it again.
func (r *relayedQuery) route(relays []*astral.Identity) (io.WriteCloser, error) {
	var answers relayAnswers

	for _, relayID := range relays {
		rw, err := r.ask(relayID)
		if err == nil {
			return rw, nil
		}
		if r.ctx.Err() != nil {
			return nil, r.ctx.Err()
		}
		answers.add(err)
	}

	return answers.result()
}

// add records one relay's answer.
func (a *relayAnswers) add(err error) {
	var reject *astral.ErrRejected
	switch {
	case errors.Is(err, &astral.ErrRouteNotFound{}):
	case errors.As(err, &reject) && reject.Code != astral.CodeUnavailable:
		if a.refused == nil {
			a.refused = reject
		}
	default:
		a.unavailable = true
	}
}

// result is the answer of a loop no relay accepted.
func (a *relayAnswers) result() (io.WriteCloser, error) {
	switch {
	case a.refused != nil:
		return nil, a.refused
	case a.unavailable:
		return nil, unavailable()
	default:
		return query.RouteNotFound()
	}
}

// ask asks one relay the query.
//
// why a refused proof is a refusal: the relay received the caller's proof and
// did not accept it (an expelled host, a caller it will not carry), and a
// retry meets the same answer.
//
// why unavailable for any other reach failure: a relay that reach did not
// reach, or could not deliver the proof to, was never asked the query, and a
// later attempt may reach it.
func (r *relayedQuery) ask(relayID *astral.Identity) (io.WriteCloser, error) {
	relay, err := r.reach(relayID)
	switch {
	case errors.Is(err, errTargetIsRelay):
		return query.RouteNotFound()
	case errors.Is(err, objects.ErrPushRejected):
		return nil, astral.NewErrRejected(astral.DefaultRejectCode)
	case err != nil:
		return nil, unavailable()
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
