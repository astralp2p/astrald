package fs

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/fs"
)

// note: an UpdatedAt of zero marks a path that is not checked yet.
// why: GORM fills a zero UpdatedAt on create, which marks an unhashed path as checked, so UpdatedAt opts out.
type dbLocalFile struct {
	ID        int64            `gorm:"primaryKey;autoIncrement"`
	Path      string           `gorm:"uniqueIndex"`
	DataID    *astral.ObjectID `gorm:"index"`
	ModTime   int64
	UpdatedAt int64 `gorm:"autoUpdateTime:false"`
	DeletedAt *time.Time
}

func (dbLocalFile) TableName() string { return fs.DBPrefix + "local_files" }
