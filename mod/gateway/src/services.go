package gateway

import (
	"context"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/gateway"
)

// Prepare registers the node as the provider of the gateway service.
func (mod *Module) Prepare(context.Context) error {
	_, err := mod.Services.RegisterNative([]string{gateway.ModuleName}, mod)
	return err
}

// Evaluate offers gateway to every caller when the node runs as a gateway.
// note: the setting is fixed for the process lifetime, so the offering never
// changes.
func (mod *Module) Evaluate(*astral.Identity, string) *services.Update {
	return &services.Update{Available: astral.Bool(mod.config.Gateway.Enabled)}
}
