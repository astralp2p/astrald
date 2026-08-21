package apphost

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/apphost"
	"time"
)

type dbAccessToken struct {
	Identity  *astral.Identity `gorm:"index"`
	Token     string           `gorm:"uniqueIndex"`
	ExpiresAt time.Time        `gorm:"index"`
}

func (dbAccessToken) TableName() string {
	return apphost.DBPrefix + "access_tokens"
}
