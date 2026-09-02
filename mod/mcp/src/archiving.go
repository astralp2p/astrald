package mcp

import (
	"github.com/astralp2p/astral-go/astral"
)

// The five things an agent does with its mail. Each mail tool is one call into
// this file: the tool names the arguments and renders the answer, and every
// decision about what the mailbox does is made on this side of the line.
//
// why the requests carry the agent's words and not the store's: resolving a
// correspondent, defaulting a list, bounding an answer and refusing a filter are
// all the module's, so a tool that did any of them would be a second place the
// rules live.

// archiveMessage puts one message away, or puts it back, and reports whether
// this call is the one that moved it.
//
// why RowsAffected is the answer: admission and write are one statement, so the
// count says both whether the message is the agent's and whether this call
// moved it. A lookup then a write would race.
//
// why the two zeroes are one answer: the same count means "already there" and
// "not yours", and separating them would tell a caller whether an id it does
// not hold exists at all. The agent's next act is the same either way.
func (mod *Module) archiveMessage(agentID *astral.Identity, ref messageRef, undo bool) (bool, error) {
	move := mod.db.Archive
	if undo {
		move = mod.db.Unarchive
	}

	n, err := move(agentID, ref.Box, ref.ID)
	if err != nil {
		return false, err
	}

	// why undo wakes and archive does not: clearing archived_at puts the row
	// back into the wait set with no insert to signal on, so it is the one
	// statement besides a delivery that adds to what a park is watching. The
	// waiter it wakes is this agent's own other session, which the endpoint
	// permits — nothing keys a session by identity.
	if undo && n == 1 {
		mod.waiters.wake(agentID)
	}

	return n == 1, nil
}
