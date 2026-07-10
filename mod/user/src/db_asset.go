package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/user"
)

type dbAsset struct {
	Nonce    astral.Nonce     `gorm:"primaryKey"`
	Removed  bool             `gorm:"index"`
	ObjectID *astral.ObjectID `gorm:"index"`
	Height   uint64           `gorm:"uniqueIndex"`
}

func (dbAsset) TableName() string { return user.DBPrefix + "assets" }
