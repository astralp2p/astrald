package vfs

import (
	"errors"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

// change is one change to a VFS: the transaction it runs in and the VFS it changes.
//
// note: vol was read before the lock, so a change uses vol.Identity and vol.RootID only.
// note: checkLimits re-reads the limits inside the transaction.
type change struct {
	tx      *DB
	vol     *dbVolume
	now     int64      // the module's clock when the change began, for the notes of replaced directories
	cleared bool       // the change cleared folder IDs
	plans   []*dirPlan // the directories the answer of the change names, stored after the commit
}

// changeVFS runs one change to a VFS: one transaction under the lock of the VFS. A change that cleared folder IDs
// makes the VFS pending. After the commit, the directories the answer names are stored.
//
// note: a change that fails rolls back, notes nothing and stores nothing.
func (mod *Module) changeVFS(vol *dbVolume, fn func(c *change) error) error {
	c := &change{vol: vol, now: mod.clock.Now().UnixNano()}

	err := mod.runLockedTxThen(vol.Identity, func(tx *DB) error {
		c.tx = tx
		return fn(c)
	}, func() {
		if c.cleared {
			mod.sched.noteChange(vol.Identity, mod.clock.Now())
		}
	})
	if err != nil || len(c.plans) == 0 {
		return err
	}

	unlock := mod.lockCompute(vol.Identity)
	defer unlock()

	for _, plan := range c.plans {
		mod.storePlanLogged(plan)
	}

	return nil
}

// clearFolderIDs clears the cached IDs from a folder up and notes the change. Every change clears through it.
func (c *change) clearFolderIDs(folderID int64) error {
	c.cleared = true

	return c.tx.clearFolderIDs(folderID, c.now)
}

// checkLimits refuses a change that grew the usage of a VFS above one of its limits.
//
// why: a limit below the usage is accepted (vfs.set_quota); it refuses growth and leaves the rest alone.
// why: the limits are read inside the change, so a vfs.set_quota acked before the change holds for it.
func (c *change) checkLimits(before vfsUsage) error {
	vol, err := c.tx.findVolume(c.vol.Identity)
	if err != nil {
		return err
	}

	after, err := c.tx.measureUsage(c.vol.Identity)
	if err != nil {
		return err
	}

	if limit := vol.byteLimit(); limit > 0 && after.Bytes > limit && after.Bytes > before.Bytes {
		return vfsapi.ErrQuotaExceeded
	}

	if limit := vol.inodeLimit(); limit > 0 && after.Inodes > limit && after.Inodes > before.Inodes {
		return vfsapi.ErrNoInodesLeft
	}

	return nil
}

// addEntry inserts a new entry into a folder. The entry and the folder get the same node time, and the folder IDs
// from the folder up are cleared. The node time is also the creation time of the entry.
//
// note: addEntry sets VFS, ParentID, ModTime and CreationTime on row.
func (c *change) addEntry(parent, row *dbEntry) error {
	now := nodeTime()

	row.VFS = parent.VFS
	row.ParentID = parent.ID
	row.ModTime = now
	row.CreationTime = now

	if err := c.tx.insertEntry(row); err != nil {
		return err
	}

	return c.markFolderChanged(parent.ID, now)
}

// markFolderChanged sets the time of a folder and clears the folder IDs from the folder up.
func (c *change) markFolderChanged(folderID, now int64) error {
	if err := c.tx.setEntryTime(folderID, now); err != nil {
		return err
	}

	return c.clearFolderIDs(folderID)
}

// makeFolders walks the names from the root of the VFS, creates every missing folder and returns the last
// folder. A file on the way answers vfsapi.ErrNotAFolder.
func (c *change) makeFolders(names []string) (*dbEntry, error) {
	row, err := c.tx.findEntry(c.vol.RootID)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		child, err := c.tx.findChild(row.ID, name)
		switch {
		case errors.Is(err, vfsapi.ErrNotFound):
			child = &dbEntry{Name: name, Flags: uint16(vfsapi.FlagFolder)}
			if err = c.addEntry(row, child); err != nil {
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

// fillEntry returns a row as an entry. A folder whose ObjectID is cleared gets it planned; changeVFS stores the
// plan after the commit.
func (c *change) fillEntry(row *dbEntry) (*vfsapi.Entry, error) {
	if !row.isFolder() || row.ObjectID != nil {
		return row.toEntry(), nil
	}

	plan, err := planFolder(c.tx, c.vol.Identity, row, 0)
	if err != nil {
		return nil, err
	}

	c.plans = append(c.plans, plan)
	row.ObjectID = &plan.ID

	return row.toEntry(), nil
}
