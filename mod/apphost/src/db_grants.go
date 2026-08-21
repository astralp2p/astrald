package apphost

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UpsertGrant records the permit for identity, replacing any grant that
// identity already held for the same action.
//
// why: replace rather than accumulate. A grant is this node's current answer
// for one identity and one action, so re-granting narrower roles must take
// roles away. Contract permits accumulate because the walk unions them; grants
// must not.
func (db *DB) UpsertGrant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error {
	row, err := fromGrantPermit(identity, permit, expiresAt)
	if err != nil {
		return err
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"data", "expires_at"}),
	}).Create(row).Error
}

// DeleteGrant withdraws identity's grant for the action.
// Returns gorm.ErrRecordNotFound when no row matches.
func (db *DB) DeleteGrant(identity *astral.Identity, action string) error {
	tx := db.
		Where("identity = ? AND name = ?", identity, action).
		Delete(&dbGrant{})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FindGrant returns the unexpired permit granted to identity for the action,
// or nil when the node has granted none.
//
// why: both sides of the expiry comparison are UTC. The pure-Go sqlite driver
// compares datetimes lexically, so a row stored in local time and queried
// against a UTC now sorts wrongly and an expired grant keeps authorizing.
func (db *DB) FindGrant(identity *astral.Identity, action string) (*auth.Permit, error) {
	var row dbGrant

	err := db.
		Where("identity = ? AND name = ?", identity, action).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		First(&row).Error

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}

	return toGrantPermit(&row)
}

// ListGrants returns every grant held for identity, expired ones included, so
// the caller can see what was granted rather than only what still applies.
func (db *DB) ListGrants(identity *astral.Identity) (list []*dbGrant, err error) {
	return list, db.Where("identity = ?", identity).Find(&list).Error
}
