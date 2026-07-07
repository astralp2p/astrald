package nodes

import (
	"slices"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/ip"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/tcp"
	"github.com/cryptopunkscc/astrald/mod/utp"
)

// ReceiveObject dispatches an inbound object by type, accepting observed-endpoint
// messages and reacting to link events with connectivity upgrades and endpoint
// refreshes. Unhandled types are ignored without accepting the drop.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	switch object := drop.Object().(type) {
	case *nodes.ObservedEndpointMessage:
		err := mod.receiveObservedEndpointMessage(drop.SenderID(), object)
		if err == nil {
			return drop.Accept(false)
		}

	case *events.Event:
		switch e := object.Data.(type) {
		case *nodes.LinkPressureEvent:
			mod.log.Log("link pressure detected on %v with %v", e.LinkID, e.RemoteIdentity)

			go mod.connectivityUpgrade(e)
		case *nodes.LinkCreatedEvent:
			if e.LinkCount == 1 && slices.ContainsFunc(mod.User.LocalSwarm(),
				e.RemoteIdentity.IsEqual) {

				go func() {
					err := mod.UpdateNodeEndpoints(mod.ctx, e.RemoteIdentity, e.RemoteIdentity)
					if err != nil {
						mod.log.Error("updating node endpoints failed: %v", err)
					}
				}()
			}

		}

	}

	return nil
}

func (mod *Module) receiveObservedEndpointMessage(source *astral.Identity, event *nodes.ObservedEndpointMessage) error {
	endpoint := event.Endpoint

	var i ip.IP
	switch e := endpoint.(type) {
	case *tcp.Endpoint:
		i = e.IP
	case *utp.Endpoint:
		i = e.IP
	default:
		// unknown endpoint type
		return nil
	}

	if i.IsPublic() {
		mod.log.Log(`public ip %v reflected from %v`, i, source)
		mod.AddObservedEndpoint(endpoint, i)
	}

	return nil
}
