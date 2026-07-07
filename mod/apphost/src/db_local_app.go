package apphost

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/apphost"
)

type dbLocalApp struct {
	AppID       *astral.Identity `gorm:"primaryKey"`
	HostID      *astral.Identity `gorm:"index"`
	InstalledAt time.Time
}

func (dbLocalApp) TableName() string { return apphost.DBPrefix + "local_apps" }
