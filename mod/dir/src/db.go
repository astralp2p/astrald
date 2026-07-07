package dir

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/dir"
)

type dbAlias struct {
	Identity *astral.Identity `gorm:"primaryKey"`
	Alias    string           `gorm:"index;unique;not null"`
}

func (dbAlias) TableName() string {
	return dir.DBPrefix + "aliases"
}
