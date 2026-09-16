package nat

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
)

// ReceiveObject re-evaluates the enabled state when a local new observed endpoint event arrives.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	switch object := drop.Object().(type) {
	case *events.Event:
		// why: an event reports this node's own state, so an event sent by another node is forged.
		if !drop.SenderID().IsEqual(mod.node.Identity()) {
			return nil
		}

		switch object.Data.(type) {
		case *nodes.NewObservedEndpointEvent:
			mod.evaluateEnabled()
		}
	}

	return nil
}
