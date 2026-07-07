package nat

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// ReceiveObject re-evaluates the enabled state when a new observed endpoint event arrives.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	switch object := drop.Object().(type) {
	case *events.Event:
		switch object.Data.(type) {
		case *nodes.NewObservedEndpointEvent:
			mod.evaluateEnabled()
		}
	}

	return nil
}
