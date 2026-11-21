package user

import (
	"errors"
	"slices"
	"sync"
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

// ReceiveObject handles pushed objects: indexes a signed node or relay-for contract,
// stores and applies a signed expulsion, syncs with the peer of a local
// LinkCreatedEvent (a sibling sync with a member, the expulsions with a banned
// subject) and with a member a local LinkClosedEvent leaves links to, contacts
// a peer of a local nearby PeerSeen, and wakes the link task of a sibling named
// by a local EndpointsUpdatedEvent.
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
			switch {
			case mod.banOn(peer) != nil:
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
		case *nodes.EndpointsUpdatedEvent:
			mod.wakeSibling(e.Identity)
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

// receiveSignedContract indexes a pushed contract that passes checkPushedContract.
// A node contract also syncs the subject's endpoints and runs the sibling linker.
// A node contract that admits a linked node to the local swarm also syncs with it.
func (mod *Module) receiveSignedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if err := mod.checkPushedContract(sender, signed); err != nil {
		mod.log.Errorv(1, "rejecting pushed contract from %v: %v", sender, err)
		return objects.ErrPushRejected
	}

	// note: "newMember", not "fresh": fresh names a node state.
	newMember := user.IsNodeContract(signed.Contract) &&
		!signed.Subject.IsEqual(mod.node.Identity()) &&
		!slices.ContainsFunc(mod.LocalSwarm(), signed.Subject.IsEqual)

	err := mod.Auth.IndexContract(mod.ctx, signed)
	if err != nil {
		mod.log.Errorv(1, "indexing signed contract failed: %v", err)
		return objects.ErrPushRejected
	}

	if !user.IsNodeContract(signed.Contract) {
		return nil
	}

	// why: the LinkCreatedEvent sync skipped a node linked before its contract
	// arrived, as on user.introduce, so the sync runs once the contract admits it.
	if newMember && mod.Nodes.IsLinked(signed.Subject) {
		mod.Scheduler.Schedule(mod.NewSyncNodesTask(signed.Subject))
	}

	go func() {
		mod.pullSubjectEndpoints(sender, signed, newMember)
		mod.runSiblingLinker()
	}()

	return nil
}

// pullSubjectEndpoints pulls the endpoints of signed's subject after its node
// contract arrived from sender.
// why: a pull from the subject itself is first-hand, so for a new member it is
// forwarded to the other siblings, as user.introduce brings the link up before
// the contract and the first-link pull skipped the subject. The siblings get the
// contract first, because they accept the notice only for a member they list.
// A contract the node already held, or one from any other sender, is not forwarded.
func (mod *Module) pullSubjectEndpoints(sender *astral.Identity, signed *auth.SignedContract, newMember bool) {
	subject := signed.Subject
	if newMember && sender.IsEqual(subject) {
		mod.pushContractToSiblings(signed)
		mod.Nodes.RefreshMemberEndpoints(subject)
		return
	}

	if _, err := mod.Nodes.UpdateNodeEndpoints(mod.ctx, sender, subject); err != nil {
		mod.log.Error("syncEndpoints: %v", err)
	}
}

// pushContractToSiblings pushes signed to every linked sibling except its subject
// and returns when each push has ended, each bounded by siblingPushTimeout.
// why: a sibling that holds no contract for the subject rejects the first-hand
// notice, and nothing else gives it the contract before the notice.
func (mod *Module) pushContractToSiblings(signed *auth.SignedContract) {
	var wg sync.WaitGroup
	for _, sib := range mod.getSiblings() {
		if sib.IsEqual(signed.Subject) {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			ctx, cancel := mod.ctx.WithTimeout(siblingPushTimeout)
			defer cancel()

			if err := mod.Objects.Push(ctx, sib, signed); err != nil {
				mod.log.Logv(1, "push contract of %v to sibling %v: %v", signed.Subject, sib, err)
			}
		}()
	}
	wg.Wait()
}

// checkPushedContract accepts two kinds of contract: a node contract issued by the
// current user, and a relay-for contract that passes AdmitRelayContract.
// Both parties must have signed it and it must not be expired.
// note: no Auth.Authorize call; a contract is checked by its parties and signatures.
func (mod *Module) checkPushedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if signed.IsNil() || signed.Issuer.IsZero() || signed.Subject.IsZero() {
		return errors.New("incomplete contract")
	}

	var err error
	switch {
	case user.IsNodeContract(signed.Contract):
		err = mod.checkNodeContract(signed)
	case isRelayForContract(signed.Contract):
		err = mod.AdmitRelayContract(sender, signed)
	default:
		err = errors.New("neither a node contract nor a relay-for contract")
	}
	if err != nil {
		return err
	}

	if err = mod.Auth.VerifyContract(signed); err != nil {
		return err
	}

	if signed.ExpiresAt.Time().Before(time.Now()) {
		return auth.ErrContractExpired
	}

	return nil
}

// checkNodeContract requires the current user as the issuer and a subject the user has not expelled.
// why: the subject need not be a swarm member yet, because this contract is what admits it.
func (mod *Module) checkNodeContract(signed *auth.SignedContract) error {
	ac := mod.ActiveContract()
	if ac == nil {
		return user.ErrNoActiveContract
	}

	if !signed.Issuer.IsEqual(ac.Issuer) {
		return errors.New("issuer is not the current user")
	}

	// why: refuse to re-index a contract for a subject the user has expelled —
	// mirrors the IssueMembership guard so a push cannot re-seat a banned node,
	// with the expelledSet filter only hiding it.
	if mod.isExpelled(ac.Issuer, signed.Subject) {
		return user.ErrExpelled
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
