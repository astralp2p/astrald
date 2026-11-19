package nodes

import (
	"net"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// Endpoint exchange in the local swarm: .ai/system/protocols/nodes/README.md#endpoint-exchange-in-the-local-swarm
const (
	// endpointsAnnounceDelay is the wait between a local address change and its announcement.
	endpointsAnnounceDelay = 5 * time.Second
	// endpointsPullTimeout bounds the pull an EndpointsChangedMessage starts.
	endpointsPullTimeout = 20 * time.Second
	// endpointsForwardTimeout bounds one forward push, as user.PushToSiblings bounds its pushes.
	endpointsForwardTimeout = 15 * time.Second
)

// changesAdvertisedAddress reports whether e adds or removes an address other
// than an IPv6 link-local one.
// why: mod/ip never advertises an IPv6 link-local address, and each container
// start adds a veth fe80::, so such a change would only cost siblings a pull.
func changesAdvertisedAddress(e *ip.EventNetworkAddressChanged) bool {
	advertised := func(a ip.IP) bool { return !(net.IP(a).IsLinkLocalUnicast() && net.IP(a).To4() == nil) }
	return slices.ContainsFunc(e.Added, advertised) || slices.ContainsFunc(e.Removed, advertised)
}

// scheduleEndpointsAnnounce starts the announcement wait unless one is running.
// why: a change during the wait joins it instead of restarting it, so a flapping
// interface costs one announcement per wait and never postpones it, and the
// receivers pull the address set as it stands after the wait.
func (mod *Module) scheduleEndpointsAnnounce() {
	if !mod.announcePending.CompareAndSwap(false, true) {
		return
	}

	time.AfterFunc(mod.announceDelay, func() {
		// note: cleared before the push, so a change during the push starts a new wait.
		mod.announcePending.Store(false)
		mod.announceEndpoints()
	})
}

// announceEndpoints pushes an EndpointsChangedMessage about this node to every linked sibling.
func (mod *Module) announceEndpoints() {
	if mod.ctx.Err() != nil {
		return
	}

	mod.log.Logv(1, "announcing changed endpoints to siblings")
	mod.User.PushToSiblings(mod.ctx, &nodes.EndpointsChangedMessage{Subject: mod.node.Identity()})
}

// receiveEndpointsChanged accepts an EndpointsChangedMessage that passes
// acceptsEndpointsChanged and pulls the subject's endpoints in the background.
func (mod *Module) receiveEndpointsChanged(sender *astral.Identity, m *nodes.EndpointsChangedMessage) error {
	if !mod.acceptsEndpointsChanged(sender, m.Subject) {
		return objects.ErrPushRejected
	}

	go mod.pullChangedEndpoints(sender, m)
	return nil
}

// acceptsEndpointsChanged reports whether sender, a linked local swarm member,
// may announce a change of subject, another local swarm member.
// why: the pull it starts runs against the sender, so a notice from an unlinked
// node or about a node outside the swarm would only cost queries.
func (mod *Module) acceptsEndpointsChanged(sender, subject *astral.Identity) bool {
	self := mod.node.Identity()
	if sender.IsZero() || sender.IsEqual(self) || !mod.IsLinked(sender) {
		return false
	}
	if subject.IsZero() || subject.IsEqual(self) {
		return false
	}

	swarm := mod.User.LocalSwarm()
	return slices.ContainsFunc(swarm, sender.IsEqual) && slices.ContainsFunc(swarm, subject.IsEqual)
}

// pullChangedEndpoints pulls m.Subject's endpoints from sender, emits an
// EndpointsUpdatedEvent when new rows arrived, and forwards a first-hand notice.
func (mod *Module) pullChangedEndpoints(sender *astral.Identity, m *nodes.EndpointsChangedMessage) {
	ctx, cancel := mod.ctx.WithTimeout(endpointsPullTimeout)
	added, err := mod.UpdateNodeEndpoints(ctx, sender, m.Subject)
	cancel()

	switch {
	case err != nil:
		mod.log.Error("pulling endpoints of %v from %v: %v", m.Subject, sender, err)
	case added > 0:
		mod.log.Logv(1, "pulled %v new endpoints of %v from %v", added, m.Subject, sender)
		mod.Events.Emit(&nodes.EndpointsUpdatedEvent{Identity: m.Subject})
	}

	// why: only a notice from its subject is forwarded, so a notice travels at most
	// one hop past the announcing node and never loops.
	// why: forwarded whatever the pull stored, because a sibling that linked to the
	// subject first already holds the new rows, and its members still need the notice.
	if sender.IsEqual(m.Subject) {
		mod.forwardEndpointsChanged(m)
	}
}

// forwardEndpointsChanged pushes a first-hand m to every linked local swarm
// member except m.Subject, its sender, each push on its own and bounded.
func (mod *Module) forwardEndpointsChanged(m *nodes.EndpointsChangedMessage) {
	self := mod.node.Identity()
	for _, member := range mod.User.LocalSwarm() {
		if member.IsEqual(self) || member.IsEqual(m.Subject) || !mod.IsLinked(member) {
			continue
		}

		go func() {
			ctx, cancel := mod.ctx.WithTimeout(endpointsForwardTimeout)
			defer cancel()

			if err := mod.Objects.Push(ctx, member, m); err != nil {
				mod.log.Logv(1, "forwarding endpoints change of %v to %v: %v", m.Subject, member, err)
			}
		}()
	}
}
