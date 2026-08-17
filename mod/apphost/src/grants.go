package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// Grant records a permit for identity on this node, replacing whatever it held
// for the same action. A nil expiresAt grants until revoked.
//
// why: Delegation is forced to zero. A grant is not portable evidence — no other
// node can read it — so passing it on is meaningless, and an issuer that wrote a
// non-zero hop count would be describing authority the grant cannot carry.
func (mod *Module) Grant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error {
	if identity == nil || identity.IsZero() {
		return apphostmod.ErrInvalidIdentity
	}
	if permit == nil || len(permit.Action) == 0 {
		return apphostmod.ErrInvalidPermit
	}

	local := *permit
	local.Delegation = 0

	return mod.db.UpsertGrant(identity, &local, expiresAt)
}

// Revoke withdraws identity's grant for the action. It takes effect on the next
// authorization; it does not undo what the identity did while it held the grant.
func (mod *Module) Revoke(identity *astral.Identity, action string) error {
	return mod.db.DeleteGrant(identity, action)
}

// Grants returns every permit this node has granted identity, expired included.
func (mod *Module) Grants(identity *astral.Identity) (list []*auth.Permit, err error) {
	rows, err := mod.db.ListGrants(identity)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		permit, err := toGrantPermit(row)
		if err != nil {
			return nil, err
		}
		list = append(list, permit)
	}

	return
}

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
