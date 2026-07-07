package user

import (
	"errors"
	"time"

	"github.com/cryptopunkscc/astral-go/api/secp256k1"
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

// Expel permanently bans nodeID from the active swarm. Only the swarm's user (the
// active contract's issuer) may expel, and the ban cannot be undone. The signed
// ban is stored, pushed to the local swarm, and the node is disconnected.
//
// Expelling suppresses swarm membership only; it does not revoke the node's
// underlying swarm-membership contract.
func (mod *Module) Expel(ctx *astral.Context, nodeID *astral.Identity) (*user.SignedExpulsion, error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil, user.ErrNoActiveContract
	}

	if nodeID.IsEqual(ac.Issuer) {
		return nil, errors.New("cannot expel the swarm user")
	}

	expulsion := &user.Expulsion{
		Issuer:     ac.Issuer,
		Subject:    nodeID,
		ExpelledAt: astral.Time(time.Now()),
	}

	sig, err := mod.Crypto.Sign(ctx, secp256k1.FromIdentity(ac.Issuer), expulsion)
	if err != nil {
		return nil, err
	}

	signed := &user.SignedExpulsion{Expulsion: expulsion, IssuerSig: sig}

	err = mod.db.StoreExpulsion(signed)
	if err != nil {
		return nil, err
	}

	go mod.PushToLocalSwarm(mod.ctx, signed)
	// why: PushToLocalSwarm and syncExpulsions both skip the subject, so push the ban
	// to the expelled node directly — its only trigger to self-enforce via leaveSwarm.
	go mod.Objects.Push(mod.ctx, nodeID, signed)
	// why: stop maintaining the link but do not force-close it — that would race the
	// push above. On delivery the node tears itself down; until then the issuer's
	// authorizers reject it.
	mod.removeSibling(nodeID)

	mod.log.Info("expelled %v from swarm", nodeID)
	return signed, nil
}

// receiveExpulsion handles an inbound signed ban. It is accepted only when issued
// by the active swarm user and carrying a valid issuer signature, then stored and
// enforced by disconnecting the subject.
func (mod *Module) receiveExpulsion(signed *user.SignedExpulsion) error {
	if signed.IsNil() {
		return objects.ErrPushRejected
	}

	ac := mod.ActiveContract()
	if ac == nil || !signed.Issuer.IsEqual(ac.Issuer) {
		return objects.ErrPushRejected
	}

	if err := mod.verifyExpulsion(signed); err != nil {
		mod.log.Errorv(1, "invalid expulsion: %v", err)
		return objects.ErrPushRejected
	}

	if err := mod.db.StoreExpulsion(signed); err != nil {
		mod.log.Errorv(1, "storing expulsion failed: %v", err)
		return objects.ErrPushRejected
	}

	mod.applyExpulsion(signed.Subject)
	return nil
}

// applyExpulsion enforces a stored ban on id. When id is the local node the ban
// strips this node's own membership via leaveSwarm; otherwise it severs ties to the
// banned peer. Membership filtering already prevents re-linking once the ban is stored.
func (mod *Module) applyExpulsion(id *astral.Identity) {
	if id.IsEqual(mod.node.Identity()) {
		mod.leaveSwarm()
		return
	}

	if err := mod.Nodes.CloseLinks(id); err != nil {
		mod.log.Errorv(1, "closing links to expelled %v: %v", id, err)
	}
	mod.removeSibling(id)
}

// leaveSwarm strips this node's own membership after it is expelled from its swarm:
// it cancels every sibling link-maintenance task, closes the links, and clears the
// active contract.
// why: expulsion does not revoke the contract, and MaintainLinkTask never re-checks
// it, so without this the banned node keeps reconnecting to and syncing with peers
// that now reject it. Clearing the contract is the only local signal of the ban.
func (mod *Module) leaveSwarm() {
	for _, sib := range mod.sibs.Values() {
		mod.removeSibling(sib.ID)
		if err := mod.Nodes.CloseLinks(sib.ID); err != nil {
			mod.log.Errorv(1, "closing link to former sibling %v: %v", sib.ID, err)
		}
	}

	if err := mod.config.ActiveContract.Clear(mod.ctx); err != nil {
		mod.log.Errorv(1, "clearing active contract after expulsion: %v", err)
	}
}

// isExpelled reports whether issuer has banned subject. Expulsion is irreversible,
// so IssueMembership refuses a banned subject rather than minting a fresh contract
// the membership filter would only hide post-hoc.
func (mod *Module) isExpelled(issuer, subject *astral.Identity) bool {
	_, banned := mod.expelledSet(issuer)[subject.String()]
	return banned
}

// expelledSet returns the subjects banned by issuer, keyed by identity string for
// O(1) membership filtering.
func (mod *Module) expelledSet(issuer *astral.Identity) map[string]struct{} {
	subjects, err := mod.db.ExpelledSubjects(issuer)
	if err != nil {
		mod.log.Errorv(1, "error reading expulsions: %v", err)
		return nil
	}

	set := make(map[string]struct{}, len(subjects))
	for _, s := range subjects {
		set[s.String()] = struct{}{}
	}
	return set
}

// verifyExpulsion checks that the issuer signature is present and valid.
func (mod *Module) verifyExpulsion(signed *user.SignedExpulsion) error {
	if signed.IssuerSig == nil {
		return errors.New("missing issuer signature")
	}

	return mod.Crypto.Verify(secp256k1.FromIdentity(signed.Issuer), signed.IssuerSig, signed.Expulsion)
}
