package mcp

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/messaging"
	"gorm.io/gorm"
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
//
// why a row already gone is not an error: mcp.list_agents drops the row of a
// withdrawn participant, and may do so between the two deletes.
func (mod *Module) deleteAgent(ctx *astral.Context, row *dbAgent) error {
	err := mod.Messaging.DeleteIdentity(ctx, row.Identity)
	if err != nil && !errors.Is(err, messaging.ErrIdentityNotFound) {
		return err
	}

	err = mod.db.DeleteAgent(row.Identity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// findAgent answers the agent row for identity, and gorm.ErrRecordNotFound for
// a row whose participant is withdrawn — see isWithdrawn. It changes no state.
//
// why the row is left: mcp.agent reads through here under SeeNodeStateAction,
// which grants no change to the state it reads. mcp.list_agents and
// mcp.delete_agent remove the row.
func (mod *Module) findAgent(identity *astral.Identity) (*dbAgent, error) {
	row, err := mod.db.FindAgent(identity)
	if err != nil {
		return nil, err
	}

	withdrawn, err := mod.isWithdrawn(row)
	if err != nil {
		return nil, err
	}
	if withdrawn {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

// isWithdrawn answers whether mod/messaging no longer names the row's
// participant. A lookup that fails answers its error and no verdict.
//
// why such a row is no agent: messaging.delete_identity withdraws the
// participant — its tokens, grants, alias and mail — and never writes this
// module's row, so the row outlives the agent with a token that no longer
// authenticates.
func (mod *Module) isWithdrawn(row *dbAgent) (bool, error) {
	err := mod.Messaging.FindIdentity(row.Identity)
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, messaging.ErrIdentityNotFound):
		return true, nil
	default:
		return false, err
	}
}

// dropWithdrawn deletes the row of an agent whose participant is withdrawn —
// see isWithdrawn — and answers whether it was one.
//
// why the row is dropped and not only skipped: a skipped row outlives every
// read, and only a delete_agent naming the bare identity reaches it, since the
// alias went with the participant.
//
// note: a failed drop still answers the agent withdrawn, and the next listing
// drops the row.
func (mod *Module) dropWithdrawn(row *dbAgent) (bool, error) {
	withdrawn, err := mod.isWithdrawn(row)
	if err != nil || !withdrawn {
		return false, err
	}

	err = mod.db.DeleteAgent(row.Identity)
	switch {
	case err == nil:
		mod.log.Logv(1, "dropped agent %v (%v): no mailbox names its participant", row.Alias, row.Identity)
	case !errors.Is(err, gorm.ErrRecordNotFound):
		mod.log.Error("agent %v: dropping the row of an agent with no mailbox: %v", row.Identity, err)
	}

	return true, nil
}
