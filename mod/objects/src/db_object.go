package objects

import (
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/objects"
)

type dbObject struct {
	// Height is a monotonic, insert-ordered tiebreaker for the (read_at, height) purge cursor.
	Height uint64           `gorm:"primaryKey;autoIncrement"`
	ID     *astral.ObjectID `gorm:"uniqueIndex"`
	// Type is the object's astral type, or NULL when unknown (blobs, raw creates).
	// why: NULL separates "type unknown" from a known-empty type, so GetType re-reads the stamp instead of caching a blank.
	Type      *string   `gorm:"index"`
	CreatedAt time.Time `gorm:"index"`
	ReadAt    time.Time `gorm:"not null;default:CURRENT_TIMESTAMP;index"`
}

func (dbObject) TableName() string { return objects.DBPrefix + "objects" }
