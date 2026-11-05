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
// recognizes nothing.
// Spec: .ai/system/protocols/nearby/README.md
func (mod *Module) RecognizeStatus(status *nearby.StatusMessage) *nearbymod.Recognition {
	userID := mod.Identity()
	if userID.IsZero() || status == nil || status.Attachments == nil {
		return nil
	}

	for _, obj := range status.Attachments.Objects() {
		if nodeID, ok := recognizeAttachment(obj, userID); ok {
			return &nearbymod.Recognition{NodeID: nodeID, UserID: userID}
		}
	}
	return nil
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
