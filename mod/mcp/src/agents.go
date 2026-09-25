package mcp

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/messaging"
)

// deleteAgent deletes the agent's messaging participant — its tokens, grants,
// alias and mail — and then its agent row.
//
// why a participant already gone is not an error: messaging.delete_identity may
// have removed it, or an earlier delete_agent may have stopped after it, and
// the row left behind must stay removable.
//
// why the participant first: a failure there keeps the row, and the row is what
// lets the operator run delete_agent again.
func (mod *Module) deleteAgent(ctx *astral.Context, row *dbAgent) error {
	err := mod.Messaging.DeleteIdentity(ctx, row.Identity)
	if err != nil && !errors.Is(err, messaging.ErrIdentityNotFound) {
		return err
	}

	return mod.db.DeleteAgent(row.Identity)
}
