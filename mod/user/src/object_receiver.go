package user

import (
	"errors"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

var _ objects.Receiver = &Module{}

// ReceiveObject handles pushed objects: indexes a signed relay-for contract,
// stores and applies a signed expulsion, syncs with the peer of a local
// LinkCreatedEvent (the first link exchanges SwarmState with its peer; a sibling sync with a member,
// the expulsions with a banned subject) and with a member a local LinkClosedEvent leaves links to, contacts
// a peer of a local nearby PeerSeen.
// Every object is checked before any effect; a rejected object is not accepted.
func (mod *Module) ReceiveObject(drop objects.Drop) (err error) {
	switch o := drop.Object().(type) {
	case *auth.SignedContract:
		err = mod.receiveSignedContract(drop.SenderID(), o)
		if err == nil {
			drop.Accept(true)
		}
	case *user.SignedExpulsion:
		err = mod.receiveExpulsion(o)
		if err == nil {
			drop.Accept(true)
		}
	case *events.Event:
		// why: an event reports this node's own state, so an event sent by another node is forged.
		if !drop.SenderID().IsEqual(mod.node.Identity()) {
			return nil
		}

		switch e := o.Data.(type) {
		case *nodes.LinkCreatedEvent:
			// note: interim re-sync; see reconcile_task.go.
			peer := e.RemoteIdentity
			// why: the exchange offers this node's membership and pulls, so it runs on the first link only;
			// the peer may be a member this node does not know yet.
			if e.LinkCount == 1 {
				go mod.onSwarmLink(peer)
			}
			switch {
			case mod.IsBanned(peer):
				// why: a banned subject is not in LocalSwarm, and its own ban is the only
				// thing it still needs from this node.
				go mod.reconcileWith(mod.ctx, peer)
			case slices.ContainsFunc(mod.LocalSwarm(), peer.IsEqual):
				// why: every link, not only the first, so a member that gains a link while
				// another one stays up still syncs.
				mod.Scheduler.Schedule(mod.NewSyncNodesTask(peer))
				drop.Accept(false)
			}
		case *nodes.LinkClosedEvent:
			if mod.onPartialLinkClose(e) {
				drop.Accept(false)
			}
		case *nearby.PeerSeen:
			// why: contact queries the peer, which must not hold up the receivers.
			go mod.onPeerSeen(e)
		}
	}

	return nil
}

// onPartialLinkClose syncs with a local swarm member when one of its links
// closed and others remain, and reports whether it did.
// why: the pool emits the event after it removed the closed link, so the sync
// goes over a survivor; the sync the replacement's LinkCreatedEvent started may
// have been routed into the dead link before it closed.
func (mod *Module) onPartialLinkClose(e *nodes.LinkClosedEvent) bool {
	if e.LinkCount == 0 || !slices.ContainsFunc(mod.LocalSwarm(), e.RemoteIdentity.IsEqual) {
		return false
	}

	id := e.RemoteIdentity
	mod.syncGate.Run(id, func() { mod.syncAndWait(id) })
	return true
}

// syncAndWait schedules a SyncNodesTask with id and waits until it ends.
func (mod *Module) syncAndWait(id *astral.Identity) {
	task, err := mod.Scheduler.Schedule(mod.NewSyncNodesTask(id))
	if err != nil {
		mod.log.Error("scheduling a sync with %v failed: %v", id, err)
		return
	}

	select {
	case <-task.Done():
	case <-mod.ctx.Done():
	}
}

// receiveSignedContract indexes a pushed relay-for contract that passes checkPushedContract.
func (mod *Module) receiveSignedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if err := mod.checkPushedContract(sender, signed); err != nil {
		mod.log.Errorv(1, "rejecting pushed contract from %v: %v", sender, err)
		return objects.ErrPushRejected
	}

	if err := mod.Auth.IndexContract(mod.ctx, signed); err != nil {
		mod.log.Errorv(1, "indexing signed contract failed: %v", err)
		return objects.ErrPushRejected
	}

	return nil
}

// checkPushedContract accepts a relay-for contract that passes AdmitRelayContract.
// Both parties must have signed it and it must not be expired.
// note: no Auth.Authorize call; a contract is checked by its parties and signatures.
func (mod *Module) checkPushedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if signed.IsNil() || signed.Issuer.IsZero() || signed.Subject.IsZero() {
		return errors.New("incomplete contract")
	}

	if !isRelayForContract(signed.Contract) {
		return errors.New("not a relay-for contract")
	}
	if err := mod.AdmitRelayContract(sender, signed); err != nil {
		return err
	}

	if err := mod.Auth.VerifyContract(signed); err != nil {
		return err
	}

	if signed.ExpiresAt.Time().Before(time.Now()) {
		return auth.ErrContractExpired
	}

	return nil
}

// isRelayForContract reports whether c carries permits and every permit is RelayForAction.
// note: this is the shape one node delivers to another; the admission rule
// covers every contract with a relay-for permit (grantsRelayFor).
func isRelayForContract(c *auth.Contract) bool {
	relayFor := nodes.RelayForAction{}.ObjectType()

	return len(c.Permits) > 0 && !slices.ContainsFunc(c.Permits, func(p *auth.Permit) bool {
		return p == nil || string(p.Action) != relayFor
	})
}
