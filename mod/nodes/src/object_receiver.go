package nodes

import (
	"slices"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
)

// ReceiveObject dispatches an inbound object by type, accepting observed-endpoint
// messages from linked peers and reacting to local link events with connectivity
// upgrades and endpoint refreshes. Unhandled and rejected objects are ignored
// without accepting the drop.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	switch object := drop.Object().(type) {
	case *nodes.ObservedEndpointMessage:
		err := mod.receiveObservedEndpointMessage(drop.SenderID(), object)
		if err == nil {
			return drop.Accept(false)
		}

	case *events.Event:
		// why: a link event reports this node's own links, so an event sent by another node is forged.
		if !drop.SenderID().IsEqual(mod.node.Identity()) {
			return nil
		}

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

// receiveObservedEndpointMessage records a public TCP endpoint that a linked peer observed for this node.
func (mod *Module) receiveObservedEndpointMessage(source *astral.Identity, event *nodes.ObservedEndpointMessage) error {
	// why: reflectLink sends an observation only to the remote identity of an inbound link,
	// so an observation from any other sender is unfounded.
	if source.IsZero() || source.IsEqual(mod.node.Identity()) || !mod.IsLinked(source) {
		return objects.ErrPushRejected
	}

	endpoint, ok := event.Endpoint.(*tcp.Endpoint)
	if !ok || endpoint == nil || !endpoint.IP.IsPublic() {
		return objects.ErrPushRejected
	}

	mod.log.Log(`public ip %v reflected from %v`, endpoint.IP, source)
	mod.AddObservedEndpoint(endpoint, endpoint.IP)

	return nil
}
