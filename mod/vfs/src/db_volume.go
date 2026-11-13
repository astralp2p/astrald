package vfs

import (
	"math"

	"github.com/astralp2p/astral-go/astral"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

// dbVolume is one VFS.
type dbVolume struct {
	ID         int64            `gorm:"primaryKey"`
	Identity   *astral.Identity `gorm:"uniqueIndex;not null"`
	RootID     int64            `gorm:"not null"` // the root row in vfs__entries
	ByteLimit  int64            `gorm:"not null"` // 0 means no limit
	EntryLimit int64            `gorm:"not null"` // 0 means no limit
}

func (dbVolume) TableName() string { return vfsmod.DBPrefix + "volumes" }

// byteLimit returns the byte limit; 0 means no limit.
func (v *dbVolume) byteLimit() uint64 { return uint64(v.ByteLimit) }

// entryLimit returns the entry limit; 0 means no limit.
func (v *dbVolume) entryLimit() uint64 { return uint64(v.EntryLimit) }

// limitColumn converts a limit for the database.
//
// why: the sql driver refuses a uint64 with the high bit set.
// note: a limit above math.MaxInt64 is stored as math.MaxInt64, about 9 exabytes.
func limitColumn(limit uint64) int64 {
	return int64(min(limit, math.MaxInt64))
}
