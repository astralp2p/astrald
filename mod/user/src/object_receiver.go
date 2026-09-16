package user

import (
	"errors"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/events"
	"github.com/astralp2p/astrald/mod/objects"
)

var _ objects.Receiver = &Module{}

// ReceiveObject handles pushed objects: indexes a signed node or relay-for contract,
// stores and applies a signed expulsion, and schedules sibling sync on a local LinkCreatedEvent.
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
			if e.LinkCount == 1 && slices.ContainsFunc(mod.LocalSwarm(), e.RemoteIdentity.IsEqual) {
				mod.Scheduler.Schedule(mod.NewSyncNodesTask(e.RemoteIdentity))
				drop.Accept(false)
			}
		}
	}

	return nil
}

// receiveSignedContract indexes a pushed contract that passes checkPushedContract.
// A node contract also syncs the subject's endpoints and runs the sibling linker.
func (mod *Module) receiveSignedContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if err := mod.checkPushedContract(sender, signed); err != nil {
		mod.log.Errorv(1, "rejecting pushed contract from %v: %v", sender, err)
		return objects.ErrPushRejected
	}

	err := mod.Auth.IndexContract(mod.ctx, signed)
	if err != nil {
		mod.log.Errorv(1, "indexing signed contract failed: %v", err)
		return objects.ErrPushRejected
	}

	if !user.IsNodeContract(signed.Contract) {
		return nil
	}

	go func() {
		err := mod.Nodes.UpdateNodeEndpoints(mod.ctx, sender, signed.Subject)
		if err != nil {
			mod.log.Error("syncEndpoints: %v", err)
		}

		mod.runSiblingLinker()
	}()

	return nil
}

// checkPushedContract accepts two kinds of contract: a node contract issued by the
// current user, and a relay-for contract whose subject is the sending sibling.
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
		err = mod.checkRelayForContract(sender, signed)
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

// checkRelayForContract requires the sender to be the contract's subject and a member of the local swarm.
// why: a node pushes its app's relay-for contract ahead of a query it relays for the app
// (mod/nodes/src/query_router.go), and the receiving node authorizes that relay from its index.
func (mod *Module) checkRelayForContract(sender *astral.Identity, signed *auth.SignedContract) error {
	if !signed.Subject.IsEqual(sender) {
		return errors.New("subject is not the sender")
	}

	if !slices.ContainsFunc(mod.LocalSwarm(), sender.IsEqual) {
		return errors.New("sender is not a swarm member")
	}

	return nil
}

// isRelayForContract reports whether c carries permits and every permit is RelayForAction.
func isRelayForContract(c *auth.Contract) bool {
	relayFor := nodes.RelayForAction{}.ObjectType()

	return len(c.Permits) > 0 && !slices.ContainsFunc(c.Permits, func(p *auth.Permit) bool {
		return p == nil || string(p.Action) != relayFor
	})
}
