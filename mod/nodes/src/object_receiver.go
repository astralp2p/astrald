package nodes

import (
	"slices"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// ReceiveObject dispatches an inbound object by type, accepting observed-endpoint
// and endpoints-changed messages from linked peers, announcing local address
// changes, waking the links bound to a removed address, and reacting to local link events with connectivity upgrades and
// endpoint refreshes, on the first link to a sibling and on the close of one of
// several links to it. Unhandled and rejected objects are ignored without
// accepting the drop.
func (mod *Module) ReceiveObject(drop objects.Drop) error {
	switch object := drop.Object().(type) {
	case *nodes.ObservedEndpointMessage:
		err := mod.receiveObservedEndpointMessage(drop.SenderID(), object)
		if err == nil {
			return drop.Accept(false)
		}

	case *nodes.EndpointsChangedMessage:
		err := mod.receiveEndpointsChanged(drop.SenderID(), object)
		if err == nil {
			return drop.Accept(false)
		}

	case *events.Event:
		// why: a link event reports this node's own links, so an event sent by another node is forged.
		if !drop.SenderID().IsEqual(mod.node.Identity()) {
			return nil
		}

		switch e := object.Data.(type) {
		case *ip.EventNetworkAddressChanged:
			// why: a link can be bound to an address that is never advertised, such as an IPv6 link-local one.
			mod.scheduleLinkWake(e)

			if changesAdvertisedAddress(e) {
				mod.scheduleEndpointsAnnounce()
			}
		case *nodes.LinkPressureEvent:
			mod.log.Log("link pressure detected on %v with %v", e.LinkID, e.RemoteIdentity)

			go mod.connectivityUpgrade(e)
		case *nodes.LinkCreatedEvent:
			// why: a gateway registration is made over a link, so a link event can follow a change of the advertised set.
			mod.scheduleEndpointsAnnounce()

			if e.LinkCount == 1 && slices.ContainsFunc(mod.User.LocalSwarm(),
				e.RemoteIdentity.IsEqual) {

				go func() {
					_, err := mod.UpdateNodeEndpoints(mod.ctx, e.RemoteIdentity, e.RemoteIdentity)
					if err != nil {
						mod.log.Error("updating node endpoints failed: %v", err)
					}
				}()
			}
		case *nodes.LinkClosedEvent:
			mod.scheduleEndpointsAnnounce()
			mod.onPartialLinkClose(e)
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

// onPartialLinkClose pulls the endpoints of a local swarm member when one of its
// links closed and others remain.
// why: the pool emits the event after it removed the closed link, so the pull
// goes over a survivor; a dead link closed after its replacement arrived is the
// reconnect the LinkCreatedEvent count missed.
func (mod *Module) onPartialLinkClose(e *nodes.LinkClosedEvent) {
	if e.LinkCount == 0 || !slices.ContainsFunc(mod.User.LocalSwarm(), e.RemoteIdentity.IsEqual) {
		return
	}

	id := e.RemoteIdentity
	mod.syncGate.Run(id, func() { mod.pullSiblingEndpoints(id) })
}
