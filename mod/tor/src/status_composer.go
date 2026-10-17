package tor

import (
	"github.com/astralp2p/astral-go/api/nearby"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

var _ nearbymod.Composer = &Module{}

// ComposeStatus attaches the local onion endpoint to the nearby composition only in ModeVisible;
// the endpoint is withheld in ModeSilent and ModeStealth.
func (mod *Module) ComposeStatus(a nearbymod.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeSilent:
		// no-op
	case nearby.ModeVisible:
		if mod.torServer == nil || mod.torServer.endpoint.IsZero() {
			return
		}
		a.Attach(mod.torServer.endpoint)
	case nearby.ModeStealth:
	}
}
