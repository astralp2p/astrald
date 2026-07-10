package fs

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/fs"
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
