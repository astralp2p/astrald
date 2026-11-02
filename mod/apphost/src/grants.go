package apphost

import (
	"time"

	apphostapi "github.com/astralp2p/astral-go/api/apphost"
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
)

// Grant adds a permit for identity on this node beside the grants it already
// holds; an identical permit refreshes its expiry. A nil expiresAt grants until
// revoked.
func (mod *Module) Grant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error {
	return mod.grantAll(identity, []*auth.Permit{permit}, expiresAt)
}

// grantAll adds every permit for identity, as Grant does, in one transaction:
// either every permit is recorded or none is.
//
// why: Delegation is forced to zero. A grant is not portable evidence — no other
// node can read it — so passing it on is meaningless, and an issuer that wrote a
// non-zero hop count would be describing authority the grant cannot carry.
func (mod *Module) grantAll(identity *astral.Identity, permits []*auth.Permit, expiresAt *time.Time) error {
	if identity == nil || identity.IsZero() {
		return apphostmod.ErrInvalidIdentity
	}

	local := make([]*auth.Permit, 0, len(permits))
	for _, permit := range permits {
		if permit == nil || len(permit.Action) == 0 {
			return apphostmod.ErrInvalidPermit
		}
		p := *permit
		p.Delegation = 0
		local = append(local, &p)
	}

	return mod.db.AddGrants(identity, local, expiresAt)
}

// Revoke withdraws every grant identity holds for the action. It takes effect on
// the next authorization; it does not undo what the identity did while it held
// the grant.
func (mod *Module) Revoke(identity *astral.Identity, action string) error {
	return mod.db.DeleteGrant(identity, action)
}

// RevokeGrant withdraws the one grant with the ID that identity holds. An ID of
// another identity's grant answers gorm.ErrRecordNotFound.
func (mod *Module) RevokeGrant(identity *astral.Identity, id uint64) error {
	return mod.db.DeleteGrantByID(identity, id)
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

// activeGrants returns the grants identity holds right now, with their IDs and
// expiries, dropping every grant that has expired.
//
// why: the wire listing reports what still authorizes, as the authorizer
// reads it; Grants reports what was granted, expired included.
func (mod *Module) activeGrants(identity *astral.Identity) (list []*apphostapi.Grant, err error) {
	rows, err := mod.db.ListActiveGrants(identity)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		permit, err := toGrantPermit(row)
		if err != nil {
			return nil, err
		}
		grant := &apphostapi.Grant{ID: astral.Uint64(row.ID), Permit: permit}
		if row.ExpiresAt != nil {
			t := astral.Time(*row.ExpiresAt)
			grant.ExpiresAt = &t
		}
		list = append(list, grant)
	}

	return
}
