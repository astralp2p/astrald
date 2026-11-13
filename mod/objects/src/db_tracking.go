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

// note: operator decision 2026-11-13: the tracking table is named objects__tracking.
func (dbTracking) TableName() string { return objects.DBPrefix + "tracking" }

// touch returns the last touch of the row: the later of its commit and read times.
func (row *dbTracking) touch() int64 {
	return max(row.CommittedAt, row.ReadAt)
}

// dbObjectsTableName names the tracking table that dbTracking replaces.
const dbObjectsTableName = objects.DBPrefix + "objects"
