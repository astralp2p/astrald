package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// authorizeGrant answers from this node's grants alone: it looks up the actor's
// permit for the action being asked and lets the permit decide.
//
// why: no action-specific logic. Narrowing lives in the permit's constraints, so
// one function serves every grantable action — a ServeObjects grant limited to
// "describer" refuses "searcher" here without this code knowing what a role is.
//
// why: a lookup error refuses. A grant reaches ops that hand out standing
// authority, so a database fault must not read as permission.
func (mod *Module) authorizeGrant(_ *astral.Context, action auth.ActionObject) bool {
	permit, err := mod.db.FindGrant(action.Actor(), action.ObjectType())
	switch {
	case err != nil:
		mod.log.Errorv(1, "grant lookup for %v: %v", action.Actor(), err)
		return false
	case permit == nil:
		return false
	}

	return permit.Allows(action)
}
