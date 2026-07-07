package scheduler

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
)

// Deps are injected by the core injector (placeholder for future module deps).
type Deps struct{}

// LoadDependencies injects required dependencies via the core injector.
func (mod *Module) LoadDependencies(*astral.Context) error {
	return core.Inject(mod.node, &mod.Deps)
}
