package mcp

import "github.com/astralp2p/astral-go/astral"

const (
	defaultInboxLimit = 50
	maxInboxLimit     = 200
)

// listInbox returns the messages waiting for the recipient, oldest first.
//
// why the cap is here and not in the tool: how much of an inbox the module
// hands out in one answer is the module's rule and not the caller's. A second
// caller — an operation serving the owner's dashboard — reads the same limit
// rather than choosing its own.
func (mod *Module) listInbox(recipient *astral.Identity, unreadOnly bool, limit int) ([]dbMessage, error) {
	if limit <= 0 {
		limit = defaultInboxLimit
	}

	return mod.db.ListInbox(recipient, unreadOnly, min(limit, maxInboxLimit))
}
