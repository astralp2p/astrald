package user

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// Expel permanently bans nodeID from the active swarm. Only the swarm's user (the
// active contract's issuer) may expel, and the ban cannot be undone. The signed
// ban is added to SwarmState, pushed to the local swarm, and the node is disconnected.
//
// Expelling does not revoke the node's underlying swarm-membership contract;
// the authorizers refuse the node whatever the contract permits (IsBanned).
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

	// why: SwarmState carries the expel to every member, and the view applies
	// it to everything signed by or for the subject (G2). It is the only store
	// of bans: list_expelled, link admission and the ban veto read it.
	if err = mod.AddExpulsion(ctx, signed); err != nil {
		return nil, err
	}
	mod.publishSwarm()

	go mod.PushToLocalSwarm(mod.ctx, signed)
	// why: PushToLocalSwarm skips the subject, so push the ban to the expelled node
	// directly — its trigger to self-enforce via leaveSwarm. A push the node
	// does not answer is not retried: the expulsion sync at the node's next link
	// with a holder of the ban and the sweep (reconcile_task.go) deliver it.
	// The node's answer closes the links with it (AdmitLink).
	// note: mod/nodes closes the subject's links when the member set changes
	// (SwarmMemberLeft); the push dials a new link, which AdmitLink admits
	// until the subject answers.
	go func() {
		if isBanAnswer(mod.pushOnce(mod.ctx, nodeID, signed)) {
			mod.onBanAnswered(nodeID)
		}
	}()

	mod.log.Info("expelled %v from swarm", nodeID)
	return signed, nil
}

// receiveExpulsion handles an inbound signed ban. It is accepted only when issued
// by the active swarm user and carrying a valid issuer signature (SwarmState
// checks it), then added to SwarmState and enforced by disconnecting the subject.
func (mod *Module) receiveExpulsion(signed *user.SignedExpulsion) error {
	if signed.IsNil() {
		return objects.ErrPushRejected
	}

	ac := mod.ActiveContract()
	if ac == nil || !signed.Issuer.IsEqual(mod.Identity()) {
		return objects.ErrPushRejected
	}

	if err := mod.AddExpulsion(mod.ctx, signed); err != nil {
		mod.log.Errorv(1, "invalid expulsion: %v", err)
		return objects.ErrPushRejected
	}
	mod.publishSwarm()

	if signed.Subject.IsEqual(mod.node.Identity()) {
		mod.leaveSwarm()
	}
	return nil
}

// leaveSwarm strips this node's own membership after it is expelled from its
// swarm: it clears the active contract, so mod/nodes stops every member's link
// task and closes the links (SwarmMemberLeft).
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
