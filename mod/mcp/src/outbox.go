package mcp

import "github.com/astralp2p/astral-go/astral"

const (
	defaultOutboxLimit = 50
	maxOutboxLimit     = 200
)

// listOutbox returns what the sender sent, newest first.
//
// why the sender is an argument and never a filter the caller supplies: an
// agent's own sends are the only ones it may read, and the identity comes from
// whoever authenticated the call.
func (mod *Module) listOutbox(sender *astral.Identity, limit int) ([]dbOutbox, error) {
	if limit <= 0 {
		limit = defaultOutboxLimit
	}

	return mod.db.ListOutbox(sender, min(limit, maxOutboxLimit))
}
