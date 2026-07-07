package gateway

import (
	"time"

	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astrald/mod/nearby"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

var _ nearby.Composer = &Module{}

// ComposeStatus attaches gateway endpoints to the nearby composition only in ModeVisible.
// Silent and stealth modes suppress all gateway advertisement.
func (mod *Module) ComposeStatus(a nearby.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeSilent:
		// no-op
	case nearby.ModeVisible:
		for _, gw := range mod.gateways.Clone() {
			a.Attach(nodes.NewEndpointWithTTL(gateway.NewEndpoint(gw, mod.node.Identity()), 7*30*24*time.Hour))
		}
	case nearby.ModeStealth:
	}
}
