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

// onPeerSeen contacts a node of the current user that nearby verified. It wakes
// the link task of a swarm member that is not linked. It asks any other such
// node for its contract and introduces this node when the contract passes
// checkPeerContract, at most once per dialInterval per node. Other nodes are
// ignored. Spec: .ai/system/protocols/user/README.md
func (mod *Module) onPeerSeen(e *nearby.PeerSeen) {
	ac := mod.ActiveContract()
	if ac == nil || e == nil {
		return
	}

	switch {
	case e.UserID.IsZero() || !e.UserID.IsEqual(ac.Issuer):
		mod.log.Logv(2, "nearby peer %v is not a node of the user", e.NodeID)
	case e.NodeID.IsZero() || e.NodeID.IsEqual(mod.node.Identity()):
		mod.log.Logv(2, "nearby peer %v is this node", e.NodeID)
	case mod.isExpelled(ac.Issuer, e.NodeID):
		mod.log.Logv(2, "nearby peer %v is expelled", e.NodeID)
	case slices.ContainsFunc(mod.LocalSwarm(), e.NodeID.IsEqual):
		mod.wakeLinkTask(e)
	case !mod.dials.reserve(e.NodeID, mod.now()):
		mod.log.Logv(2, "nearby peer %v was contacted recently", e.NodeID)
	default:
		mod.contactPeer(ac, e)
	}
}

// wakeLinkTask makes the link task of a swarm member seen nearby link now,
// unless the member is linked.
func (mod *Module) wakeLinkTask(e *nearby.PeerSeen) {
	if mod.Nodes.IsLinked(e.NodeID) {
		mod.log.Logv(2, "nearby peer %v is a linked sibling", e.NodeID)
		return
	}

	mod.log.Log("sibling %v is nearby at %v, linking now", e.NodeID, e.SourceIP)
	if !mod.wakeSibling(e.NodeID) {
		// note: a member without a link task gets one, and a new task links at once.
		mod.runSiblingLinker()
	}
}

// contactPeer reads the contract of a peer of the current user and introduces
// this node to the peer when the contract passes checkPeerContract. A failed
// read or check sends nothing of this node's own and holds off the next attempt
// for dialIntervalAfterRefusal.
func (mod *Module) contactPeer(ac *auth.SignedContract, e *nearby.PeerSeen) {
	// why: the query is this node's own, and its link to the peer goes out to the network.
	ctx, cancel := mod.ctx.IncludeZone(astral.ZoneNetwork).WithTimeout(peerContractTimeout)
	defer cancel()

	signed, err := mod.readPeerContract(ctx, e.NodeID)
	if err == nil {
		err = mod.checkPeerContract(e.NodeID, signed, mod.now())
	}
	if err != nil {
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
// a node contract the current user issued to peer, both signatures valid, not
// expired at now, and peer not expelled.
func (mod *Module) checkPeerContract(peer *astral.Identity, signed *auth.SignedContract, now time.Time) error {
	switch {
	case signed.IsNil() || signed.Issuer.IsZero() || signed.Subject.IsZero():
		return errors.New("incomplete contract")
	case slices.Contains(signed.Permits, nil):
		// why: the answer comes from the network, and IsNodeContract reads every permit.
		return errors.New("contract holds an empty permit")
	case !user.IsNodeContract(signed.Contract):
		return errors.New("not a node contract")
	case !signed.Subject.IsEqual(peer):
		return errors.New("subject is not the peer")
	}

	if err := mod.checkNodeContract(signed); err != nil {
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
