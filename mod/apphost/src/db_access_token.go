package apphost

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/apphost"
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
