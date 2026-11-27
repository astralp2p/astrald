package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
	"gorm.io/gorm"
)

// Expel permanently bans nodeID from the active swarm. Only the swarm's user (the
// active contract's issuer) may expel, and the ban cannot be undone. The signed
// ban is stored, pushed to the local swarm, and the node is disconnected.
//
// Expelling does not revoke the node's underlying swarm-membership contract;
// the authorizers refuse the node whatever the contract permits (isBanned).
func (mod *Module) Expel(ctx *astral.Context, nodeID *astral.Identity) (*user.SignedExpulsion, error) {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil, user.ErrNoActiveContract
	}

	if nodeID.IsEqual(mod.Identity()) {
		return nil, errors.New("cannot expel the swarm user")
	}

	expulsion := &user.Expulsion{
		Issuer:     mod.Identity(),
		Subject:    nodeID,
		ExpelledAt: astral.Time(time.Now()),
	}

	sig, err := mod.Crypto.Sign(ctx, secp256k1.FromIdentity(mod.Identity()), expulsion)
	if err != nil {
		return nil, err
	}

	signed := &user.SignedExpulsion{Expulsion: expulsion, IssuerSig: sig}

	err = mod.db.StoreExpulsion(signed)
	if err != nil {
		return nil, err
	}
	// why: SwarmState carries the expel to every member, and the view applies
	// it to everything signed by or for the subject (G2).
	if err = mod.AddExpulsion(ctx, signed); err != nil {
		mod.log.Error("expel %v: swarm state: %v", nodeID, err)
	}
	mod.publishBan(nodeID)

	go mod.PushToLocalSwarm(mod.ctx, signed)
	// why: PushToLocalSwarm skips the subject, so push the ban to the expelled node
	// directly — its trigger to self-enforce via leaveSwarm. The push is retried
	// until the node answers; the expulsion sync at the node's next link with a
	// holder of the ban and the sweep (reconcile_task.go) also deliver it.
	// The node's answer closes the links with it (AdmitLink).
	// note: mod/nodes closes the subject's links on the SwarmMemberLeft that
	// publishBan emits; the push dials a new link, which AdmitLink admits
	// until the subject answers.
	go func() {
		if mod.pushUntilDelivered(mod.ctx, nodeID, signed) {
			mod.onBanAnswered(nodeID)
		}
	}()

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
	if ac == nil || !signed.Issuer.IsEqual(mod.Identity()) {
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
	mod.publishBan(signed.Subject)

	mod.applyExpulsion(signed.Subject)
	return nil
}

// applyExpulsion enforces a stored ban on id. When id is the local node the ban
// strips this node's own membership via leaveSwarm. The links of a banned
// peer are closed by mod/nodes on the SwarmMemberLeft that publishBan
// emitted, and membership filtering prevents re-linking.
func (mod *Module) applyExpulsion(id *astral.Identity) {
	if id.IsEqual(mod.node.Identity()) {
		mod.leaveSwarm()
	}
}

// leaveSwarm strips this node's own membership after it is expelled from its
// swarm: it clears the active contract. Every member then leaves LocalSwarm,
// so mod/nodes stops every member's link task and closes the links
// (SwarmMemberLeft).
// why: expulsion does not revoke the contract, and a member link task never
// re-checks it, so without this the banned node keeps reconnecting to and
// syncing with peers that now reject it. Clearing the contract is the only
// local signal of the ban.
func (mod *Module) leaveSwarm() {
	mod.log.Info("leaving the swarm: this node is expelled")

	if err := mod.config.ActiveContract.Clear(mod.ctx); err != nil {
		mod.log.Errorv(1, "clearing active contract after expulsion: %v", err)
	}
}

// banOn returns the ban the active contract's issuer issued on subject, or nil
// when there is no active contract or no such ban.
func (mod *Module) banOn(subject *astral.Identity) *user.SignedExpulsion {
	ac := mod.ActiveContract()
	if ac == nil {
		return nil
	}

	ban, err := mod.db.ExpulsionFor(mod.Identity(), subject)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			mod.log.Errorv(1, "reading the ban on %v: %v", subject, err)
		}
		return nil
	}
	return ban
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
	set, err := mod.readExpelledSet(issuer)
	if err != nil {
		mod.log.Errorv(1, "error reading expulsions: %v", err)
		return nil
	}
	return set
}

// readExpelledSet is expelledSet with the error of the ban store read.
func (mod *Module) readExpelledSet(issuer *astral.Identity) (map[string]struct{}, error) {
	subjects, err := mod.db.ExpelledSubjects(issuer)
	if err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(subjects))
	for _, s := range subjects {
		set[s.String()] = struct{}{}
	}
	return set, nil
}

// verifyExpulsion checks that the issuer signature is present and valid.
func (mod *Module) verifyExpulsion(signed *user.SignedExpulsion) error {
	if signed.IssuerSig == nil {
		return errors.New("missing issuer signature")
	}

	return mod.Crypto.Verify(secp256k1.FromIdentity(signed.Issuer), signed.IssuerSig, signed.Expulsion)
}
