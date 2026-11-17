package user

import (
	"errors"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dbMnemonic records the object that holds the BIP-39 words of one user.
//
// why only the object ID: the words live in the object repository beside the
// user's key; the row finds them without a search.
type dbMnemonic struct {
	UserID   *astral.Identity `gorm:"primaryKey"`
	ObjectID *astral.ObjectID
}

func (dbMnemonic) TableName() string { return user.DBPrefix + "mnemonics" }

// SetMnemonic records objectID as the mnemonic of userID, replacing any
// earlier record, so a repeat user.bootstrap leaves one row.
func (db *DB) SetMnemonic(userID *astral.Identity, objectID *astral.ObjectID) error {
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&dbMnemonic{
		UserID:   userID,
		ObjectID: objectID,
	}).Error
}

// MnemonicID returns the object ID of the mnemonic of userID, or nil when none
// is recorded.
func (db *DB) MnemonicID(userID *astral.Identity) (*astral.ObjectID, error) {
	var row dbMnemonic
	err := db.Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.ObjectID, nil
}

// DeleteMnemonic removes the record of the mnemonic of userID.
func (db *DB) DeleteMnemonic(userID *astral.Identity) error {
	return db.Where("user_id = ?", userID).Delete(&dbMnemonic{}).Error
}
