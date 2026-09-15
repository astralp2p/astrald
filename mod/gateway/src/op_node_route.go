package gateway

import (
	"context"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opNodeRouteArgs struct {
	Identity string `query:"required"`
}

// OpNodeRoute establishes a routed connection to the named node: if it is this node, accepts the connection as an inbound link;
// otherwise forwards the connection to the next hop and pipes both sides.
func (mod *Module) OpNodeRoute(ctx *astral.Context, q *routing.IncomingQuery, args opNodeRouteArgs) error {
	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	// note: a name that does not resolve is refused the same way an unauthorized
	// forward is, so the refusal tells a caller nothing about this node's directory.
	target, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil || target.IsZero() {
		return q.Reject()
	}

	// target is this node — accept and establish inbound link
	if target.IsEqual(mod.node.Identity()) {
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

	// why: forwarding uses the gateway service; accepting an inbound link above does not.
	if !mod.authorizeUseGateway(ctx, q) {
		return q.Reject()
	}

	// forward: accept caller side, dial target side, pipe
	inConn := q.AcceptRaw()
	// why: the next hop receives the resolved identity, so it never reinterprets a
	// name under its own directory.
	nextQ := query.New(mod.node.Identity(), target, gateway.MethodNodeRoute, query.Args{"identity": target})
	outConn, err := query.RouteInFlight(ctx, mod.node, astral.Launch(nextQ))
	if err != nil {
		inConn.Close()
		return err
	}

	go pipe(inConn, outConn)
	return nil
}
