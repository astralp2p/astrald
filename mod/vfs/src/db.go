package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"gorm.io/gorm"
)

type DB struct {
	*gorm.DB
}

func (db *DB) migrate() error {
	return db.AutoMigrate(&dbVolume{}, &dbEntry{})
}

// inTx runs fn in one transaction and commits it when fn returns nil.
func (db *DB) inTx(fn func(tx *DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error { return fn(&DB{tx}) })
}

// findVolume returns the VFS of an identity, or vfsapi.ErrNoSuchVFS.
func (db *DB) findVolume(id *astral.Identity) (*dbVolume, error) {
	var rows []dbVolume

	// why: Find with a limit, not First, so a miss logs no "record not found" line.
	err := db.Where("identity = ?", id).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNoSuchVFS
	}

	return &rows[0], nil
}

// getEntry returns the row of an entry, or vfsapi.ErrNotFound.
func (db *DB) getEntry(rowID int64) (*dbEntry, error) {
	var rows []dbEntry

	err := db.Where("id = ?", rowID).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNotFound
	}

	return &rows[0], nil
}

// findChild returns the entry called name in a folder, or vfsapi.ErrNotFound.
func (db *DB) findChild(parentID int64, name string) (*dbEntry, error) {
	var rows []dbEntry

	// note: the root is its own parent and has an empty name, which no valid name equals.
	err := db.Where("parent_id = ? AND name = ?", parentID, name).Limit(1).Find(&rows).Error
	switch {
	case err != nil:
		return nil, err
	case len(rows) == 0:
		return nil, vfsapi.ErrNotFound
	}

	return &rows[0], nil
}

// listChildren returns the entries of a folder in ascending byte order of name.
func (db *DB) listChildren(parentID int64) (rows []*dbEntry, err error) {
	// note: the root is its own parent, so the query leaves the root row itself out.
	err = db.Where("parent_id = ? AND id <> parent_id", parentID).Order("name").Find(&rows).Error

	return
}

// resolveEntry walks the names from the root of a VFS and returns the row they name. No names give the root row.
//
// A missing name answers vfsapi.ErrNotFound. A file in the middle of the walk answers vfsapi.ErrNotAFolder. The
// last name may be a file or a folder.
func (db *DB) resolveEntry(vol *dbVolume, names []string) (*dbEntry, error) {
	row, err := db.getEntry(vol.RootID)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		if !row.isFolder() {
			return nil, vfsapi.ErrNotAFolder
		}

		if row, err = db.findChild(row.ID, name); err != nil {
			return nil, err
		}
	}

	return row, nil
}

// usage returns the bytes of file content a VFS holds. Each distinct ObjectID counts once, and a folder counts
// nothing.
//
// why: usage is computed from the entries, so no column can drift from them.
func (db *DB) usage(vfsID *astral.Identity) (total uint64, err error) {
	rows, err := db.Model(&dbEntry{}).
		Where("vfs = ? AND flags & 1 = 0", vfsID).
		Distinct("object_id").
		Rows()
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var id astral.ObjectID
		if err = rows.Scan(&id); err != nil {
			return 0, err
		}
		total += id.Size
	}

	return total, rows.Err()
}

// entryCount returns the number of entries of a VFS. The root is not counted.
func (db *DB) entryCount(vfsID *astral.Identity) (count uint64, err error) {
	var n int64

	err = db.Model(&dbEntry{}).Where("vfs = ? AND id <> parent_id", vfsID).Count(&n).Error

	return uint64(n), err
}
