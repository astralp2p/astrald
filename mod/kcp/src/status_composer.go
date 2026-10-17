package kcp

import (
	"github.com/astralp2p/astral-go/api/nearby"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

var _ nearbymod.Composer = &Module{}

// ComposeStatus attaches KCP endpoints to the nearby composition only in
// ModeVisible; silent and stealth modes contribute nothing.
func (mod *Module) ComposeStatus(a nearbymod.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeSilent:
		// no-op
	case nearby.ModeVisible:
		endpoints := mod.endpoints()
		for _, endpoint := range endpoints {
			a.Attach(endpoint)
		}
	case nearby.ModeStealth:
	}
}
