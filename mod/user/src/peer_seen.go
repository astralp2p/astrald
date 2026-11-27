package user

import (
	"errors"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/user"
	userClient "github.com/astralp2p/astral-go/api/user/client"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
)

// peerContractTimeout bounds the user.contract query to a peer found nearby,
// the link to the peer included.
const peerContractTimeout = 30 * time.Second

// peerContractReader returns the active contract of peer (user.contract).
type peerContractReader func(ctx *astral.Context, peer *astral.Identity) (*auth.SignedContract, error)

// onPeerSeen contacts a node of the current user that nearby verified. A swarm
// member is left to mod/nodes, which links it. It asks any other such
// node for its contract and introduces this node when the contract passes
// checkPeerContract, at most once per dialInterval per node. Other nodes are
// ignored. Spec: .ai/system/protocols/user/README.md
func (mod *Module) onPeerSeen(e *nearby.PeerSeen) {
	// why: nearby emits PeerSeen before this module is ready, and Run sets the
	// context of the contact before it closes ready.
	<-mod.ready

	ac := mod.ActiveContract()
	if ac == nil || e == nil {
		return
	}

	switch {
	case e.UserID.IsZero() || !e.UserID.IsEqual(mod.Identity()):
		mod.log.Logv(2, "nearby peer %v is not a node of the user", e.NodeID)
	case e.NodeID.IsZero() || e.NodeID.IsEqual(mod.node.Identity()):
		mod.log.Logv(2, "nearby peer %v is this node", e.NodeID)
	case mod.Expelled(e.NodeID):
		mod.log.Logv(2, "nearby peer %v is expelled", e.NodeID)
	case slices.ContainsFunc(mod.LocalSwarm(), e.NodeID.IsEqual):
		mod.log.Logv(2, "nearby peer %v is a swarm member", e.NodeID)
	case !mod.dials.reserve(e.NodeID, mod.now()):
		mod.log.Logv(2, "nearby peer %v was contacted recently", e.NodeID)
	default:
		mod.contactPeer(ac, e)
	}
}

// contactPeer reads the contract of a peer of the current user and introduces
// this node to the peer when the contract passes checkPeerContract. A failed
// read or check sends nothing of this node's own. A failed check holds off the
// next attempt for dialIntervalAfterRefusal; a failed read keeps the
// dialInterval that reserve set.
func (mod *Module) contactPeer(ac *auth.SignedContract, e *nearby.PeerSeen) {
	// why: the query is this node's own, and its link to the peer goes out to the network.
	ctx, cancel := mod.ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(peerContractTimeout)
	defer cancel()

	signed, err := mod.readPeerContract(ctx, e.NodeID)
	if err != nil {
		mod.log.Log("reading the contract of nearby peer %v at %v failed: %v", e.NodeID, e.SourceIP, err)
		return
	}
	if err := mod.checkPeerContract(e.NodeID, signed, mod.now()); err != nil {
		mod.dials.delay(e.NodeID, mod.now().Add(dialIntervalAfterRefusal))
		mod.log.Log("not introducing to nearby peer %v at %v: %v", e.NodeID, e.SourceIP, err)
		return
	}

	if err := mod.introduce(mod.ctx, ac, e.NodeID); err != nil {
		mod.log.Log("introducing to nearby peer %v at %v failed: %v", e.NodeID, e.SourceIP, err)
		return
	}
	mod.log.Log("introduced to nearby peer %v at %v", e.NodeID, e.SourceIP)
}

// readContract asks peer for its active contract over a link from this node.
func (mod *Module) readContract(ctx *astral.Context, peer *astral.Identity) (*auth.SignedContract, error) {
	return userClient.New(peer, core.NewClientAs(mod.node, mod.node.Identity())).Contract(ctx)
}

// checkPeerContract checks the contract that peer answered user.contract with:
// a membership the current user issued to peer, both signatures valid, not
// expired at now, and peer not expelled.
func (mod *Module) checkPeerContract(peer *astral.Identity, signed *auth.SignedContract, now time.Time) error {
	switch {
	case signed.IsNil() || signed.Issuer.IsZero() || signed.Subject.IsZero():
		return errors.New("incomplete contract")
	case slices.Contains(signed.Permits, nil):
		// why: the answer comes from the network, and ContractTier reads every permit.
		return errors.New("contract holds an empty permit")
	}
	if _, ok := user.ContractTier(signed.Contract); !ok {
		return errors.New("not a membership")
	}
	if !signed.Subject.IsEqual(peer) {
		return errors.New("subject is not the peer")
	}

	if err := mod.checkMembership(signed); err != nil {
		return err
	}
	if err := mod.Auth.VerifyContract(signed); err != nil {
		return err
	}
	if signed.ExpiresAt.Time().Before(now) {
		return auth.ErrContractExpired
	}
	return nil
}

// checkMembership requires the current user as the issuer and a subject the user has not expelled.
// why: the subject need not be a swarm member yet, because this contract is what admits it.
func (mod *Module) checkMembership(signed *auth.SignedContract) error {
	if mod.ActiveContract() == nil {
		return user.ErrNoActiveContract
	}

	if !signed.Issuer.IsEqual(mod.Identity()) {
		return errors.New("issuer is not the current user")
	}

	// why: refuse a contract for a subject the user has expelled; the member
	// filter would only hide it.
	if mod.Expelled(signed.Subject) {
		return user.ErrExpelled
	}

	return nil
}
