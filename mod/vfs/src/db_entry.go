package vfs

import (
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	vfsmod "github.com/astralp2p/astrald/mod/vfs"
)

// dbEntry is one entry of a VFS, or the root folder of one.
//
// The root row is its own parent: ParentID equals ID. Every other row has a real parent, so a parent is never
// NULL and a walk up ends at the row that is its own parent.
type dbEntry struct {
	ID  int64            `gorm:"primaryKey"`
	VFS *astral.Identity `gorm:"not null;index:idx_vfs__entries_vfs_object,priority:1"`
	// why: the name has binary collation, so equality and order are byte by byte.
	ParentID int64  `gorm:"not null;uniqueIndex:idx_vfs__entries_parent_name,priority:1"`
	Name     string `gorm:"type:text collate binary;not null;uniqueIndex:idx_vfs__entries_parent_name,priority:2"`
	Flags    uint16 `gorm:"not null"`

	// ObjectID is the content ObjectID of a file. For a folder it is the cached ObjectID of the listing,
	// or NULL when cleared.
	ObjectID *astral.ObjectID `gorm:"index;index:idx_vfs__entries_vfs_object,priority:2"`

	// ModTime is nanoseconds since the Unix epoch, the value that is hashed.
	ModTime int64 `gorm:"not null"`
}

func (dbEntry) TableName() string { return vfsmod.DBPrefix + "entries" }

// isFolder reports whether the folder bit is set.
func (e *dbEntry) isFolder() bool { return astral.Uint16(e.Flags)&vfsapi.FlagFolder != 0 }

// isRoot reports whether the row is the root of a VFS.
func (e *dbEntry) isRoot() bool { return e.ParentID == e.ID }

// toEntry returns the entry as it is listed. The ID of a folder is the cached one: a caller that needs it
// computes it first.
func (e *dbEntry) toEntry() *vfsapi.Entry {
	entry := &vfsapi.Entry{
		Name:    astral.String8(e.Name),
		Flags:   astral.Uint16(e.Flags),
		ModTime: astral.Time(time.Unix(0, e.ModTime).UTC()),
	}
	if e.ObjectID != nil {
		entry.ID = *e.ObjectID
	}

	return entry
}

// modTimeColumn converts a time for the database: nanoseconds since the Unix epoch.
func modTimeColumn(t time.Time) int64 { return t.UnixNano() }

// nodeTime returns the node's time as a modification time column.
func nodeTime() int64 { return modTimeColumn(time.Now()) }
