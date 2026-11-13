package vfs

import (
	"errors"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// changeAuthorize authorizes a write on a VFS and returns it. A nil VFS is the caller's.
func (mod *Module) changeAuthorize(ctx *astral.Context, q *routing.IncomingQuery, vfsID *astral.Identity) (*dbVolume, error) {
	if vfsID == nil {
		vfsID = q.Caller()
	}

	return mod.authorizeVFS(ctx, q.Caller(), vfsID, accessWrite)
}

// changeVFS runs one change to a VFS: one transaction under the lock of the VFS.
//
// note: fn calls no other module and no repository; the node has one database connection.
func (mod *Module) changeVFS(vol *dbVolume, fn func(tx *DB) error) error {
	unlock := mod.lockVFS(vol.Identity)
	defer unlock()

	return mod.db.inTx(fn)
}

// changeNow returns the node's time as a modification time column.
func changeNow() int64 { return modTimeColumn(time.Now()) }

// changeUsage is what the limits of a VFS measure.
type changeUsage struct {
	Bytes   uint64
	Entries uint64
}

// changeMeasure returns the usage of a VFS.
func (db *DB) changeMeasure(vfsID *astral.Identity) (u changeUsage, err error) {
	if u.Bytes, err = db.usage(vfsID); err != nil {
		return
	}

	u.Entries, err = db.entryCount(vfsID)

	return
}

// changeCheckLimits refuses a change that grew the usage of a VFS above one of its limits.
//
// why: a limit below the usage is accepted (vfs.set_quota); it refuses growth and leaves the rest alone.
// why: the limits are read inside the change, so a vfs.set_quota acked before the change holds for it.
func (db *DB) changeCheckLimits(vfsID *astral.Identity, before changeUsage) error {
	vol, err := db.findVolume(vfsID)
	if err != nil {
		return err
	}

	after, err := db.changeMeasure(vfsID)
	if err != nil {
		return err
	}

	if limit := vol.byteLimit(); limit > 0 && after.Bytes > limit && after.Bytes > before.Bytes {
		return vfsapi.ErrQuotaExceeded
	}

	if limit := vol.entryLimit(); limit > 0 && after.Entries > limit && after.Entries > before.Entries {
		return vfsapi.ErrTooManyEntries
	}

	return nil
}

// changeSetTime sets the modification time of one row.
func (db *DB) changeSetTime(rowID int64, modTime int64) error {
	return db.Model(&dbEntry{}).Where("id = ?", rowID).Update("mod_time", modTime).Error
}

// changeAddEntry inserts a new entry into a folder. The entry and the folder get the node's time, and the
// folder IDs from the folder up are cleared.
func (db *DB) changeAddEntry(parent *dbEntry, row *dbEntry) error {
	now := changeNow()

	row.VFS = parent.VFS
	row.ParentID = parent.ID
	row.ModTime = now

	if err := db.Create(row).Error; err != nil {
		return err
	}

	if err := db.changeSetTime(parent.ID, now); err != nil {
		return err
	}

	return db.clearFolderIDs(parent.ID)
}

// changeMakeFolders walks the names from the root of a VFS, creates every missing folder and returns the last
// folder. A file on the way answers vfsapi.ErrNotAFolder.
func (db *DB) changeMakeFolders(vol *dbVolume, names []string) (*dbEntry, error) {
	row, err := db.getEntry(vol.RootID)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		child, err := db.findChild(row.ID, name)
		switch {
		case errors.Is(err, vfsapi.ErrNotFound):
			child = &dbEntry{Name: name, Flags: uint16(vfsapi.FlagFolder)}
			if err = db.changeAddEntry(row, child); err != nil {
				return nil, err
			}
		case err != nil:
			return nil, err
		}

		if !child.isFolder() {
			return nil, vfsapi.ErrNotAFolder
		}

		row = child
	}

	return row, nil
}

// changeEntry returns a row as an entry. A folder whose ObjectID is cleared gets it computed and cached.
//
// The caller runs inside a change, under the lock of the VFS.
func (db *DB) changeEntry(row *dbEntry) (*vfsapi.Entry, error) {
	if row.isFolder() && row.ObjectID == nil {
		id, filled, err := db.computeFolderID(row.ID)
		if err != nil {
			return nil, err
		}

		if err = db.storeFolderIDs(filled); err != nil {
			return nil, err
		}

		row.ObjectID = &id
	}

	return row.entry(), nil
}
