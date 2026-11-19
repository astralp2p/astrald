package user

import (
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
	nearbymod "github.com/astralp2p/astrald/mod/nearby"
)

var _ nearbymod.Recognizer = &Module{}

// RecognizeStatus recognizes a status of a node of the current user: a
// StealthHint that opens with the user, or a PublicProfile that names the user
// and a node. It returns the node and the user, or nil. A node without a user
// recognizes only the node user.join is joining; see recognizeJoinTarget.
// Spec: .ai/system/protocols/nearby/README.md
func (mod *Module) RecognizeStatus(status *nearby.StatusMessage) *nearbymod.Recognition {
	if status == nil || status.Attachments == nil {
		return nil
	}
	userID := mod.Identity()
	if userID.IsZero() {
		return mod.recognizeJoinTarget(status)
	}

	for _, obj := range status.Attachments.Objects() {
		if nodeID, ok := recognizeAttachment(obj, userID); ok {
			return &nearbymod.Recognition{NodeID: nodeID, UserID: userID}
		}
	}
	return nil
}

// recognizeJoinTarget recognizes the stealth hint of the node user.join is
// joining: the hint opens with that node's identity to its user. It returns nil
// while no join runs.
//
// why: nearby still checks the proof's nonce and its signature under the
// target's key, so a replayed hint never resolves the target to another IP.
// Spec: .ai/system/protocols/user/ops/user.join.md
func (mod *Module) recognizeJoinTarget(status *nearby.StatusMessage) *nearbymod.Recognition {
	target := mod.joinTarget.Load()
	if target == nil {
		return nil
	}
	hint, ok := astral.First[*nearby.StealthHint](status.Attachments.Objects())
	if !ok {
		return nil
	}
	userID, ok := hint.OpenWithNode(target)
	if !ok {
		return nil
	}
	return &nearbymod.Recognition{NodeID: target, UserID: userID}
}

// recognizeAttachment returns the node that obj names as a node of userID.
func recognizeAttachment(obj astral.Object, userID *astral.Identity) (*astral.Identity, bool) {
	switch a := obj.(type) {
	case *nearby.StealthHint:
		return a.Open(userID)
	case *nearby.PublicProfile:
		if a == nil || a.NodeID.IsZero() || a.UserID.IsZero() || !a.UserID.IsEqual(userID) {
			return nil, false
		}
		return a.NodeID, true
	}
	return nil, false
}
