package fs

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/fs"
)

type dbLocalFile struct {
	ID        int64            `gorm:"primaryKey;autoIncrement"`
	Path      string           `gorm:"uniqueIndex"`
	DataID    *astral.ObjectID `gorm:"index"`
	ModTime   int64
	UpdatedAt int64
	DeletedAt *time.Time
}

func (dbLocalFile) TableName() string { return fs.DBPrefix + "local_files" }
