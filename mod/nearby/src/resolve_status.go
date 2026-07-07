package nearby

import (
	"bytes"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/nearby"
)

// ResolveStatus extracts the sender identity from a StatusMessage using three strategies
// in priority order: visible mode (PublicProfile or SignedContract attachment), then
// stealth mode (commitment verification and XOR-unmasking against the local user identity).
func (mod *Module) ResolveStatus(status *nearby.StatusMessage) *astral.Identity {
	// visible mode: identity from signed contract
	if c, ok := astral.First[*nearby.PublicProfile](status.Attachments.Objects()); ok {
		return c.NodeID
	}

	if c, ok := astral.First[*auth.SignedContract](status.Attachments.Objects()); ok {
		return c.Subject
	}

	// stealth mode: verify commitment and unmask
	if hint, ok := astral.First[*nearby.StealthHint](status.Attachments.Objects()); ok {
		userID := mod.Deps.User.Identity()
		if userID == nil {
			return nil
		}

		if !bytes.Equal(nearby.ComputeCommitment(userID, hint.Nonce), hint.Commitment) {
			return nil
		}

		id, err := nearby.UnmaskIdentity(hint.MaskedID, userID)
		if err != nil {
			return nil
		}
		return id
	}

	return nil
}
