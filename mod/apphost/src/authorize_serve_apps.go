package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// authorizeServeApps reports whether host may host on this node.
//
// apphost.register_handler and mod.apphost.register_service_msg both ask this
// question for the identity a handler answers for. Each refuses when the
// answer is no, before it installs a handler.
//
// note: ServeApps authorizes adding the caller's own handler. It does not
// authorize removing another identity's handler.
func (mod *Module) authorizeServeApps(ctx *astral.Context, host *astral.Identity) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeAppsAction{Action: auth.NewAction(host)})
}
