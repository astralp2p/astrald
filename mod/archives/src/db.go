package archives

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/archives"
	"time"
)

type dbArchive struct {
	ID        uint             `gorm:"primarykey"`
	ObjectID  *astral.ObjectID `gorm:"uniqueIndex"`
	Entries   []dbEntry        `gorm:"OnDelete:CASCADE;foreignKey:ParentID"`
	Format    string           `gorm:"index"`
	Comment   string
	CreatedAt time.Time
}

func (dbArchive) TableName() string { return archives.DBPrefix + "archives" }

type dbEntry struct {
	Parent   *dbArchive
	ParentID uint             `gorm:"primaryKey"`
	Path     string           `gorm:"primaryKey"`
	ObjectID *astral.ObjectID `gorm:"index"`
	Comment  string
	Modified time.Time
}

func (dbEntry) TableName() string { return archives.DBPrefix + "entries" }
