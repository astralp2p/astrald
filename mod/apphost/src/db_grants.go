package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AddGrant records the permit for identity beside the grants it already holds.
// An identical permit is stored once: granting it again sets that row's expiry
// to expiresAt.
//
// why: the conflict target is the whole permit, not the action. Several grants
// for one action add up, so an identity can be granted scopes step by step;
// re-granting the same permit only moves its expiry.
func (db *DB) AddGrant(identity *astral.Identity, permit *auth.Permit, expiresAt *time.Time) error {
	row, err := fromGrantPermit(identity, permit, expiresAt)
	if err != nil {
		return err
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity"}, {Name: "name"}, {Name: "data"}},
		DoUpdates: clause.AssignmentColumns([]string{"expires_at"}),
	}).Create(row).Error
}

// AddGrants records every permit for identity, as AddGrant does, in one
// transaction: either every permit is recorded or none is.
func (db *DB) AddGrants(identity *astral.Identity, permits []*auth.Permit, expiresAt *time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, permit := range permits {
			if err := (&DB{tx}).AddGrant(identity, permit, expiresAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteGrant withdraws every grant identity holds for the action.
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

// DeleteGrantByID withdraws the one grant with the ID, when identity holds it.
// Returns gorm.ErrRecordNotFound when no row matches.
//
// why the identity is matched too: an ID read from another identity's listing
// must delete nothing.
func (db *DB) DeleteGrantByID(identity *astral.Identity, id uint64) error {
	tx := db.
		Where("id = ? AND identity = ?", id, identity).
		Delete(&dbGrant{})
	if tx.Error != nil {
		return tx.Error
	}
	if tx.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// FindGrants returns every unexpired permit granted to identity for the
// action, oldest first, or none when the node has granted none.
//
// why: both sides of the expiry comparison are UTC. The pure-Go sqlite driver
// compares datetimes lexically, so a row stored in local time and queried
// against a UTC now sorts wrongly and an expired grant keeps authorizing.
func (db *DB) FindGrants(identity *astral.Identity, action string) (list []*auth.Permit, err error) {
	var rows []*dbGrant

	err = db.
		Where("identity = ? AND name = ?", identity, action).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		Order("id").
		Find(&rows).Error
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

	return list, nil
}

// ListGrants returns every grant held for identity, expired ones included, so
// the caller can see what was granted rather than only what still applies.
func (db *DB) ListGrants(identity *astral.Identity) (list []*dbGrant, err error) {
	return list, db.Where("identity = ?", identity).Order("id").Find(&list).Error
}

// ListActiveGrants returns the grants held for identity that have not expired.
//
// why: the expiry predicate is FindGrants', so a listing and an authorization
// answer the same question. Both sides of the comparison are UTC, for the
// reason FindGrants records.
func (db *DB) ListActiveGrants(identity *astral.Identity) (list []*dbGrant, err error) {
	return list, db.
		Where("identity = ?", identity).
		Where("expires_at IS NULL OR expires_at > ?", time.Now().UTC()).
		Order("id").
		Find(&list).Error
}
