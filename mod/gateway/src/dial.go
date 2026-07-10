package gateway

import (
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	gatewayClient "github.com/astralp2p/astral-go/api/gateway/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/astrald"
	"github.com/astralp2p/astral-go/lib/query"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	gatewaymod "github.com/astralp2p/astrald/mod/gateway"
)

var _ exonetmod.Dialer = &Module{}

// Dial connects to a gateway endpoint by first attempting the fast socket path
// (reserve an idle connection via the gateway's Connect RPC, then dial the raw
// socket), and falling back to the slower link-routed path if either step fails.
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (exonetmod.Conn, error) {
	if endpoint.Network() != NetworkName {
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	gwEndpoint, ok := endpoint.(*gateway.Endpoint)
	if !ok {
		return nil, exonetmod.ErrUnsupportedNetwork
	}

	if gwEndpoint.GatewayID.IsEqual(mod.node.Identity()) {
		return nil, gatewaymod.ErrInvalidGateway
	}

	ctx = ctx.IncludeZone(astral.ZoneNetwork)

	client := gatewayClient.New(gwEndpoint.GatewayID, astrald.Default())
	socket, err := client.Connect(ctx, gwEndpoint.TargetID)
	if err != nil {
		return mod.route(ctx, gwEndpoint)
	}

	conn, err := mod.Exonet.Dial(ctx, socket.Endpoint)
	if err != nil {
		return mod.route(ctx, gwEndpoint)
	}

	ch := channel.New(conn)
	err = ch.Send(&socket.Nonce)
	if err != nil {
		conn.Close()
		return mod.route(ctx, gwEndpoint)
	}

	return &gatewayConn{
		ReadWriteCloser: conn,
		local:           gateway.NewEndpoint(mod.node.Identity(), mod.node.Identity()),
		remote:          gwEndpoint,
		outbound:        conn.Outbound(),
	}, nil
}

func (mod *Module) route(ctx *astral.Context, gwEndpoint *gateway.Endpoint) (exonetmod.Conn, error) {
	mod.log.Logv(1, "socket path unavailable, trying link path to %v via %v", gwEndpoint.TargetID, gwEndpoint.GatewayID)

	q := query.New(mod.node.Identity(), gwEndpoint.GatewayID, gateway.MethodNodeRoute, query.Args{"target": gwEndpoint.TargetID})

	conn, err := query.RouteInFlight(ctx, mod.node, astral.Launch(q))
	if err != nil {
		return nil, err
	}

	return &gatewayConn{
		ReadWriteCloser: conn,
		local:           gateway.NewEndpoint(mod.node.Identity(), mod.node.Identity()),
		remote:          gwEndpoint,
		outbound:        true,
	}, nil
}
