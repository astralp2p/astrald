package objects

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// dbTracking is one tracked object in one leaf repository (VFS 6, §11 "objects: tracking").
// note: both times are whole seconds since the Unix epoch. ReadAt is 0 until the first counted read.
type dbTracking struct {
	Repository  string           `gorm:"primaryKey"`
	ObjectID    *astral.ObjectID `gorm:"primaryKey"`
	CommittedAt int64            `gorm:"not null"`
	ReadAt      int64            `gorm:"not null;default:0"`
}

// note: provisional, pending operator decision: the name of the tracking table.
func (dbTracking) TableName() string { return objects.DBPrefix + "tracking" }

// dbObjectsTableName names the tracking table that dbTracking replaces.
const dbObjectsTableName = objects.DBPrefix + "objects"
