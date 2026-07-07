package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func (db *DB) CreateAccessToken(identity *astral.Identity, d astral.Duration) (token *dbAccessToken, err error) {
	var expiresAt = (astral.Time)(time.Now().Add(time.Duration(d)))

	token = &dbAccessToken{
		Identity:  identity,
		Token:     randomString(32),
		ExpiresAt: time.Time(expiresAt),
	}

	err = db.Create(token).Error

	return
}

func (db *DB) ListAccessTokens() (list []dbAccessToken, _ error) {
	return list, db.Find(&list).Error
}

func (db *DB) FindAccessToken(token string) (at *dbAccessToken, err error) {
	err = db.
		Where("token = ?", token).
		First(&at).Error
	return
}

func (db *DB) CreateLocalApp(appID, hostID *astral.Identity) error {
	row := &dbLocalApp{AppID: appID, HostID: hostID, InstalledAt: time.Now()}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

func (db *DB) ListLocalApps() (list []*dbLocalApp, err error) {
	return list, db.Find(&list).Error
}

// HoldObject records that appID wants objectID retained.
// A nil duration creates a permanent hold; a non-nil duration sets an expiry.
// Duplicate holds are silently ignored (ON CONFLICT DO NOTHING).
func (db *DB) HoldObject(appID *astral.Identity, objectID *astral.ObjectID, duration *astral.Duration) error {
	// note: apps may hold object IDs before this node has fetched the object.
	row := &dbObjectHold{AppID: appID, ObjectID: objectID, CreatedAt: time.Now()}
	if duration != nil {
		until := time.Now().Add(time.Duration(*duration))
		row.HoldUntil = &until
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error
}

func (db *DB) UnholdObject(appID *astral.Identity, objectID *astral.ObjectID) error {
	return db.
		Where("app_id = ? AND object_id = ?", appID, objectID).
		Delete(&dbObjectHold{}).
		Error
}

// ListHeldObjects returns only the non-expired holds for appID.
func (db *DB) ListHeldObjects(appID *astral.Identity) (list []*dbObjectHold, err error) {
	err = db.
		Where("app_id = ?", appID).
		Where("(hold_until IS NULL OR hold_until > ?)", time.Now()).
		Find(&list).
		Error
	return
}

// ObjectHeld reports whether any app currently holds objectID (expired holds are excluded).
func (db *DB) ObjectHeld(objectID *astral.ObjectID) (held bool, err error) {
	err = db.
		Model(&dbObjectHold{}).
		Where("object_id = ?", objectID).
		Where("(hold_until IS NULL OR hold_until > ?)", time.Now()).
		Select("count(*) > 0").
		First(&held).
		Error
	return
}
