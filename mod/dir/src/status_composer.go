package dir

import (
	"github.com/cryptopunkscc/astral-go/api/dir"
	"github.com/cryptopunkscc/astrald/mod/nearby"
)

var _ nearby.Composer = &Module{}

// ComposeStatus attaches the node's directory alias to the nearby composition when the node is in visible mode.
// Silent and stealth modes produce no attachment.
func (mod *Module) ComposeStatus(a nearby.Composition) {
	switch mod.Nearby.Mode() {
	case nearby.ModeSilent:
		// no-op
	case nearby.ModeVisible:
		alias, err := mod.GetAlias(mod.node.Identity())
		if err == nil && alias != "" {
			v := dir.Alias(alias)
			a.Attach(&v)
		}
	case nearby.ModeStealth:
	}
}
