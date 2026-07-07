package user

import (
	"slices"

	"github.com/cryptopunkscc/astral-go/api/auth"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/user"
)

var _ objects.Receiver = &Module{}

// ReceiveObject handles pushed objects: indexes incoming signed contracts and triggers sibling sync on new node-contract arrivals.
// Accepts a LinkCreatedEvent from a swarm member to kick off endpoint synchronization without persisting the event.
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
		switch e := o.Data.(type) {
		case *nodes.LinkCreatedEvent:
			if e.LinkCount == 1 && slices.ContainsFunc(mod.LocalSwarm(), e.RemoteIdentity.IsEqual) {
				mod.Scheduler.Schedule(mod.NewSyncNodesTask(e.RemoteIdentity))
				drop.Accept(false)
			}
		}
	}

	return nil
}

func (mod *Module) receiveSignedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	ac := mod.ActiveContract()

	isIssuerUser := ac != nil && signed.Issuer.IsEqual(ac.Issuer)
	isSubjectSwarmMember := slices.ContainsFunc(mod.LocalSwarm(), signed.Subject.IsEqual)
	isIssuerSwarmMember := slices.ContainsFunc(mod.LocalSwarm(), signed.Issuer.IsEqual)

	if !(isIssuerUser || isSubjectSwarmMember || isIssuerSwarmMember) {
		return objects.ErrPushRejected
	}

	// why: refuse to re-index a contract for a subject the issuer has expelled —
	// mirrors the IssueMembership guard so an unsynced or hostile member cannot
	// re-seat a banned node through a push, with the expelledSet filter only hiding it.
	if mod.isExpelled(signed.Issuer, signed.Subject) {
		mod.log.Errorv(1, "rejecting pushed contract for expelled %v", signed.Subject)
		return objects.ErrPushRejected
	}

	if err := mod.Auth.VerifyContract(signed); err != nil {
		mod.log.Errorv(1, "invalid signed contract: %v", err)
		return objects.ErrPushRejected
	}

	err := mod.Auth.IndexContract(mod.ctx, signed)
	if err != nil {
		mod.log.Errorv(1, "indexing signed contract failed: %v", err)
		return objects.ErrPushRejected
	}

	go func() {
		if !user.IsNodeContract(signed.Contract) {
			return
		}

		err := mod.Nodes.UpdateNodeEndpoints(mod.ctx, sender, signed.Subject)
		if err != nil {
			mod.log.Error("syncEndpoints: %v", err)
		}

		mod.runSiblingLinker()
	}()

	return nil
}
