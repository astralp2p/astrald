package nodes

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/mod/nodes"
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

	for _, relayID := range relays {
		// never use the target as a relay to itself
		if relayID.IsEqual(q.Target) {
			continue
		}

		link := mod.linkPool.SelectLinkWith(relayID)
		if link == nil {
			result := <-mod.linkPool.RetrieveLink(retrieveCtx, relayID, WithStrategies(nodes.StrategyBasic, nodes.StrategyTor))
			if result.Err != nil {
				continue
			}
			link = result.Link
		}

		if !ctx.Identity().IsEqual(q.Caller) {
			if err := mod.sendCallerProof(ctx, q, relayID); err != nil {
				continue
			}
		}

		rw, err := link.RouteQuery(ctx, q, w)
		if err == nil {
			return rw, nil
		}
	}

	return query.RouteNotFound()
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
