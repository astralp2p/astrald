package gateway

import (
	"context"

	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/lib/query"
	"github.com/cryptopunkscc/astrald/lib/routing"
)

type opNodeRouteArgs struct {
	Target *astral.Identity
}

// OpNodeRoute establishes a routed connection to target: if target is this node, accepts the connection as an inbound link;
// otherwise forwards the connection to the next hop and pipes both sides.
func (mod *Module) OpNodeRoute(ctx *astral.Context, q *routing.IncomingQuery, args opNodeRouteArgs) error {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	// target is this node — accept and establish inbound link
	if args.Target.IsEqual(mod.node.Identity()) {
		conn := q.AcceptRaw()
		c := &gatewayConn{
			ReadWriteCloser: conn,
			local:           gateway.NewEndpoint(q.Target(), q.Target()),
			remote:          gateway.NewEndpoint(q.Caller(), q.Target()),
		}

		actx, cancel := context.WithTimeout(context.Background(), acceptTimeout)
		defer cancel()

		if err := mod.Nodes.EstablishInboundLink(actx, c); err != nil {
			mod.log.Errorv(1, "inbound link from %v failed: %v", q.Caller(), err)
		}
		return nil
	}

	// forward: accept caller side, dial target side, pipe
	inConn := q.AcceptRaw()
	nextQ := query.New(mod.node.Identity(), args.Target, gateway.MethodNodeRoute, query.Args{"target": args.Target})
	outConn, err := query.RouteInFlight(ctx, mod.node, astral.Launch(nextQ))
	if err != nil {
		inConn.Close()
		return err
	}

	go pipe(inConn, outConn)
	return nil
}
