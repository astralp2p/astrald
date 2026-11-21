package nodes

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
	"time"
)

type dbEndpoint struct {
	Identity  *astral.Identity `gorm:"primaryKey"`
	Network   string           `gorm:"primaryKey"`
	Address   string           `gorm:"primaryKey"`
	CreatedAt time.Time
	ExpiresAt *time.Time `gorm:"index"`
	// Manual marks a row an operator added with nodes.add_endpoint.
	// note: a pull never shortens a manual row; the subject need not list it.
	Manual bool `gorm:"not null;default:false"`
}

func (dbEndpoint) TableName() string {
	return nodes.DBPrefix + "endpoints"
}
