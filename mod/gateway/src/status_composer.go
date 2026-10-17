package gateway

import (
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

var _ nearbymod.Composer = &Module{}

// ComposeStatus attaches gateway endpoints to the nearby composition only in ModeVisible.
// Silent and stealth modes suppress all gateway advertisement.
func (mod *Module) ComposeStatus(a nearbymod.Composition) {
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
