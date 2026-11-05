package nearby

import (
	"errors"
	"fmt"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

// The StatusProof signature lengths a receiver accepts: a DER ECDSA secp256k1 signature.
const (
	minSigLen = 8
	maxSigLen = 72
)

// The reasons a received status is dropped.
var (
	errPanic           = errors.New("panic while handling the status")
	errNoBundle        = errors.New("status has no attachments")
	errIdentityCount   = errors.New("status needs exactly one public_profile or stealth_hint")
	errProofPlacement  = errors.New("status_proof is not the one last attachment")
	errSigLength       = errors.New("status_proof signature length is out of range")
	errStale           = errors.New("status timestamp is out of the freshness window")
	errCacheFull       = errors.New("cache is full")
	errVerifiedHeld    = errors.New("source IP holds a verified status")
	errNoNodeID        = errors.New("status names no node")
	errNodeMismatch    = errors.New("recognized node differs from the public_profile node")
	errOwnStatus       = errors.New("status of the local node")
	errReplay          = errors.New("status nonce was seen from the node")
	errNodeNonces      = errors.New("node holds too many live status nonces")
	errNonceMemoryFull = errors.New("nonce memory is full")
	errBadProof        = errors.New("status proof does not verify")
)

// receivedStatus is a status that passed the shape check, split into its parts.
type receivedStatus struct {
	status  *nearby.StatusMessage
	profile *nearby.PublicProfile // nil for a stealth status
	others  []astral.Object       // every attachment but the proof
	proof   *nearby.StatusProof
}

// identified names the sender of a status; via is nearby.ViaHint or nearby.ViaProfile.
type identified struct {
	node *astral.Identity
	user *astral.Identity // nil when unknown
	via  string
}

// acceptStatus runs the receive checks on a status from src in the spec's order and caches
// it when every check passes. A panic drops the status. See protocols/nearby/README.md.
func (mod *Module) acceptStatus(src ip.IP, status *nearby.StatusMessage) (err error) {
	// why: a status is untrusted input, so a panic while handling it costs that status only.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errPanic, r)
		}
	}()

	now := mod.now()
	r, err := checkShape(status)
	if err != nil {
		return err
	}
	if !isFresh(r.proof.Timestamp, now) {
		return errStale
	}
	if !mod.cache.hasRoom(src.String()) {
		return errCacheFull
	}

	id, err := mod.identify(r)
	if err != nil {
		return err
	}
	if id == nil {
		return mod.storeUnrecognised(src, status, now)
	}

	if err = mod.verify(r, id, now); err != nil {
		return err
	}
	return mod.storeVerified(&entry{NodeID: id.node, UserID: id.user, IP: src, Via: id.via, Seen: now, Status: status})
}

// checkShape splits a status into its parts. It refuses a status without exactly one
// public profile or stealth hint, without one proof as its last attachment, or with a
// signature length out of range.
func checkShape(status *nearby.StatusMessage) (*receivedStatus, error) {
	if status == nil || status.Attachments == nil {
		return nil, errNoBundle
	}

	objs := status.Attachments.Objects()
	profiles := astral.SelectByType[*nearby.PublicProfile](objs)
	hints := astral.SelectByType[*nearby.StealthHint](objs)
	if len(profiles)+len(hints) != 1 {
		return nil, errIdentityCount
	}

	others, proof, ok := nearby.SplitProof(objs)
	if !ok {
		return nil, errProofPlacement
	}
	if n := len(proof.Sig); n < minSigLen || n > maxSigLen {
		return nil, errSigLength
	}

	r := &receivedStatus{status: status, others: others, proof: proof}
	if len(profiles) == 1 {
		r.profile = profiles[0]
	}
	return r, nil
}

// isFresh reports whether ts lies within statusTTL of now, on either side.
//
// note: time comparisons, not a duration: a subtraction of a far timestamp saturates.
func isFresh(ts astral.Time, now time.Time) bool {
	t := ts.Time()
	return !t.Before(now.Add(-statusTTL)) && !t.After(now.Add(statusTTL))
}

// identify names the sender of a status: the first recognizer that recognizes it, else
// the public profile. It returns nil for a stealth status no recognizer recognizes.
func (mod *Module) identify(r *receivedStatus) (*identified, error) {
	via := nearby.ViaHint
	if r.profile != nil {
		via = nearby.ViaProfile
	}

	var node, user *astral.Identity
	rec := mod.recognize(r.status)
	switch {
	case rec == nil && r.profile == nil:
		return nil, nil
	case rec == nil:
		node, user = r.profile.NodeID, r.profile.UserID
	case rec.NodeID.IsZero():
		return nil, errNoNodeID
	case r.profile != nil && !rec.NodeID.IsEqual(r.profile.NodeID):
		return nil, errNodeMismatch
	default:
		node, user = rec.NodeID, rec.UserID
	}

	switch {
	case node.IsZero():
		return nil, errNoNodeID
	case node.IsEqual(mod.node.Identity()):
		// why: a replay of the node's own status from another IP would cache the node as its own peer.
		return nil, errOwnStatus
	case user.IsZero():
		user = nil
	}
	return &identified{node: node, user: user, via: via}, nil
}

// recognize asks the recognizers in turn; the first that recognizes the status wins. It
// returns nil when none does.
func (mod *Module) recognize(status *nearby.StatusMessage) *nearbymod.Recognition {
	for _, r := range mod.recognizers.Clone() {
		if rec := r.RecognizeStatus(status); rec != nil {
			return rec
		}
	}
	return nil
}

// verify checks that the proof's nonce is new for the node and that its signature, unwrapped
// in stealth, verifies under the node's key; it then remembers the nonce.
func (mod *Module) verify(r *receivedStatus, id *identified, now time.Time) error {
	if mod.nonces.seen(id.node, r.proof.Nonce, now) {
		return errReplay
	}

	hash, err := nearby.ProofHash(r.others, r.proof.Nonce, r.proof.Timestamp)
	if err != nil {
		return fmt.Errorf("%w: %w", errBadProof, err)
	}

	sig := []byte(r.proof.Sig)
	if r.profile == nil {
		sig = nearby.WrapProofSig(id.node, r.proof.Nonce, sig)
	}

	err = mod.Crypto.VerifyHashSignature(
		secp256k1.FromIdentity(id.node),
		&crypto.Signature{Scheme: crypto.SchemeASN1, Data: sig},
		hash,
	)
	if err != nil {
		return fmt.Errorf("%w: %w", errBadProof, err)
	}

	return mod.rememberNonce(id.node, r.proof, now)
}

// rememberNonce remembers the nonce of proof for node. A full memory first forgets the
// nonces of every node other than node that holds no live cache entry.
//
// why: a flood under many keys then costs the flooder's keys their nonces, not every other node its statuses.
// note: a replay of a forgotten node's status can cache that node again; a cached node keeps its nonces, so no replay moves it.
func (mod *Module) rememberNonce(node *astral.Identity, proof *nearby.StatusProof, now time.Time) error {
	err := mod.nonces.remember(node, proof, now)
	if !errors.Is(err, errNonceMemoryFull) {
		return err
	}

	keep := mod.cache.liveNodes(now)
	keep[node.String()] = true
	mod.nonces.retain(keep, now)
	return mod.nonces.remember(node, proof, now)
}

// storeUnrecognised caches a stealth status no recognizer recognizes, with its source IP only.
func (mod *Module) storeUnrecognised(src ip.IP, status *nearby.StatusMessage, now time.Time) error {
	return mod.cache.storeUnrecognised(&entry{IP: src, Via: nearby.ViaHint, Seen: now, Status: status})
}

// storeVerified caches a verified status and emits PeerSeen when the node is new to the
// cache, moved to another IP, or returns after expiry; a Silent node with silent_listen
// off emits nothing.
func (mod *Module) storeVerified(e *entry) error {
	isNew, stored := mod.cache.storeVerified(e)
	if !stored {
		return errCacheFull
	}
	if !isNew || (mod.Mode() == nearby.ModeSilent && !mod.listensInSilent()) {
		return nil
	}

	mod.Events.Emit(&nearby.PeerSeen{
		NodeID:   e.NodeID,
		UserID:   e.UserID,
		Via:      astral.String8(e.Via),
		SourceIP: e.IP,
	})
	return nil
}
