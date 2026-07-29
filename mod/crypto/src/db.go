package crypto

import (
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DB struct {
	*gorm.DB
}

func newDB(gormDB *gorm.DB) (*DB, error) {
	db := &DB{gormDB}

	err := db.DB.AutoMigrate(&dbPrivateKey{})

	if err != nil {
		return nil, err
	}
	return db, nil
}

func (db *DB) findPrivateKeyByPublicKey(pubKey string) (row dbPrivateKey, err error) {
	err = db.
		Where("public_key = ?", pubKey).First(&row).Error

	return
}

func (db *DB) isKeyIndexed(keyID *astral.ObjectID) (exist bool, err error) {
	err = db.
		Model(&dbPrivateKey{}).
		Where("key_id = ? OR public_key_id = ?", keyID, keyID).
		Select("count(*) > 0").
		First(&exist).
		Error
	return
}

func (db *DB) createPrivateKey(keyID *astral.ObjectID, typ string, pubKeyID *astral.ObjectID, pubKey string) (*dbPrivateKey, error) {
	row := &dbPrivateKey{
		KeyID:       keyID,
		Type:        typ,
		PublicKeyID: pubKeyID,
		PublicKey:   pubKey,
	}

	// why: the register op indexes its fresh key while the repo follower
	// auto-indexes the same just-stored object; the loser of that race must
	// land as a no-op, not a UNIQUE constraint error sent back to the app.
	err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error

	return row, err
}
