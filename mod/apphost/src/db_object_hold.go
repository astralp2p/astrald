package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

type dbObjectHold struct {
	AppID     *astral.Identity `gorm:"primaryKey;index"`
	ObjectID  *astral.ObjectID `gorm:"primaryKey;index"`
	HoldUntil *time.Time       `gorm:"index"`
	CreatedAt time.Time        `gorm:"index"`
}

func (dbObjectHold) TableName() string {
	return apphost.DBPrefix + "object_holds"
}
