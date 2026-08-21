package dir

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/dir"
)

type dbAlias struct {
	Identity *astral.Identity `gorm:"primaryKey"`
	Alias    string           `gorm:"index;unique;not null"`
}

func (dbAlias) TableName() string {
	return dir.DBPrefix + "aliases"
}
