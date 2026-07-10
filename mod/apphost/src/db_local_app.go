package apphost

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/apphost"
)

type dbLocalApp struct {
	AppID       *astral.Identity `gorm:"primaryKey"`
	HostID      *astral.Identity `gorm:"index"`
	InstalledAt time.Time
}

func (dbLocalApp) TableName() string { return apphost.DBPrefix + "local_apps" }
