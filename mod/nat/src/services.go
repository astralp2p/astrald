package nat

import (
	"context"

	"github.com/astralp2p/astral-go/api/services"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nat"
)

// Prepare registers the node as the provider of the nat service.
func (mod *Module) Prepare(context.Context) error {
	reg, err := mod.Services.RegisterNative([]string{nat.ModuleName}, mod)
	if err != nil {
		return err
	}
	mod.services = reg
	return nil
}

// Evaluate offers nat to every caller while traversal is enabled.
func (mod *Module) Evaluate(*astral.Identity, string) *services.Update {
	return &services.Update{Available: astral.Bool(mod.enabled.Load())}
}
