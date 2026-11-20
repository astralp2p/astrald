package nodes

import (
	"crypto/sha256"
	"net"
	"slices"
	"strings"
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

// endpointsHash fingerprints a set of endpoints; see ownEndpointsHash.
type endpointsHash [sha256.Size]byte

// scheduleEndpointsAnnounce starts the announcement wait unless one is running.
// It is the entry point of every trigger: an advertised address change, a link
// created or closed, the startup settle, and the daily sibling refresh of EP4.
// why: a change during the wait joins it instead of restarting it, so a flapping
// interface costs one announcement per wait and never postpones it, and the
// receivers pull the address set as it stands after the wait.
func (mod *Module) scheduleEndpointsAnnounce() {
	if !mod.announcePending.CompareAndSwap(false, true) {
		return
	}

	time.AfterFunc(mod.announceDelay, func() {
		// note: cleared before the check, so a trigger during the push starts a new wait.
		mod.announcePending.Store(false)
		mod.announceIfEndpointsChanged()
	})
}

// announceIfEndpointsChanged announces this node's endpoints to its siblings
// when their fingerprint differs from the last announced one, and does nothing
// otherwise. The first call after start always announces.
// why: the fingerprint is taken after the wait, so a burst of triggers compares
// the settled set, and a trigger that changed nothing costs the siblings no pull.
// note: the fingerprint is stored before the push, and a push with no linked
// sibling still consumes it. A sibling that links later pulls on its first link.
func (mod *Module) announceIfEndpointsChanged() {
	if mod.ctx.Err() != nil {
		return
	}

	h, err := mod.ownEndpointsHash()
	if err != nil {
		mod.log.Error("fingerprinting own endpoints: %v", err)
		return
	}

	// note: nil means nothing was announced yet, so no fingerprint equals it.
	if prev := mod.lastAnnouncedHash.Swap(&h); prev != nil && *prev == h {
		return
	}

	mod.announceEndpoints()
}

// ownEndpointsHash returns the SHA-256 of the sorted, deduplicated
// "network:address" strings that ResolveEndpoints returns for this node's own
// identity. TTLs and the order of arrival are ignored.
func (mod *Module) ownEndpointsHash() (endpointsHash, error) {
	ch, err := mod.ResolveEndpoints(mod.ctx, mod.node.Identity())
	if err != nil {
		return endpointsHash{}, err
	}

	var keys []string
	for e := range ch {
		keys = append(keys, e.Network()+":"+e.Address())
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)

	return sha256.Sum256([]byte(strings.Join(keys, "\n"))), nil
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
