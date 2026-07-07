package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/nodes"
	"time"
)

type dbEndpoint struct {
	Identity  *astral.Identity `gorm:"primaryKey"`
	Network   string           `gorm:"primaryKey"`
	Address   string           `gorm:"primaryKey"`
	CreatedAt time.Time
	ExpiresAt *time.Time `gorm:"index"`
}

func (dbEndpoint) TableName() string {
	return nodes.DBPrefix + "endpoints"
}
