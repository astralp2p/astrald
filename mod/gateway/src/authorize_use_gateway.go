package gateway

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// AuthorizeUseGateway allows every actor to use this node's gateway service
// while the gateway is enabled, and no actor while it is disabled.
//
// why: the gateway is a public service; no grant, contract, or swarm membership stands behind its use.
// note: the action grants nothing auth.AdminNetworkAction governs.
func (mod *Module) AuthorizeUseGateway(_ *astral.Context, _ *auth.UseGatewayAction) bool {
	return mod.canGateway()
}

// authorizeUseGateway reports whether the query's caller may use this node's
// gateway service. Registration, connection reservation, and forwarding ask
// it and reject the query on no, before they accept the query or change state.
//
// why: Auth.Authorize continues past a false handler into contracts and external authorities.
// why: the enabled check precedes it, so a disabled gateway refuses whatever another path allows.
func (mod *Module) authorizeUseGateway(ctx *astral.Context, q *routing.IncomingQuery) bool {
	if !mod.canGateway() {
		return false
	}

	return mod.Auth.Authorize(ctx, &auth.UseGatewayAction{Action: auth.NewAction(q.Caller())})
}
