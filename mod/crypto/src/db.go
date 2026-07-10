package crypto

import (
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
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

	err := db.Create(row).Error

	return row, err
}
