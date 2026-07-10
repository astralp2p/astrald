package tcp

import (
	"github.com/astralp2p/astrald/mod/nearby"
)

var _ nearby.Composer = &Module{}

// ComposeStatus attaches the module's TCP endpoints to the nearby composition only in ModeVisible;
// silent and stealth modes produce no entries.
func (mod *Module) ComposeStatus(a nearby.Composition) {
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
