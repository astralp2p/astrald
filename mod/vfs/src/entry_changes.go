package vfs

import (
	"errors"
	"slices"
	"time"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
)

// createFolder creates the folder the names give, with its missing parents.
func (c *change) createFolder(names []string) error {
	// note: the root always exists.
	if len(names) == 0 {
		return nil
	}

	before, err := c.tx.measureUsage(c.vol.Identity)
	if err != nil {
		return err
	}

	parent, err := c.makeFolders(names[:len(names)-1])
	if err != nil {
		return err
	}

	row, err := c.tx.findChild(parent.ID, names[len(names)-1])
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		row = &dbEntry{Name: names[len(names)-1], Flags: uint16(vfsapi.FlagFolder)}
		if err = c.addEntry(parent, row); err != nil {
			return err
		}
	case err != nil:
		return err
	case !row.isFolder():
		return vfsapi.ErrAlreadyExists
	}

	return c.checkLimits(before)
}

// moveEntry moves the entry at from to the path to.
func (c *change) moveEntry(from, to []string) (*vfsapi.Entry, error) {
	// why: the root cannot be moved (operator decision 2026-11-12).
	// note: operator decision 2026-11-13: a move onto the root answers invalid path, not already exists.
	if len(from) == 0 || len(to) == 0 {
		return nil, vfsapi.ErrInvalidPath
	}

	before, err := c.tx.measureUsage(c.vol.Identity)
	if err != nil {
		return nil, err
	}

	row, err := c.tx.resolveEntry(c.vol, from)
	if err != nil {
		return nil, err
	}

	if row.isFolder() && isBeneath(from, to) {
		return nil, vfsapi.ErrInvalidPath
	}

	parent, err := c.makeFolders(to[:len(to)-1])
	if err != nil {
		return nil, err
	}

	if err = c.relinkEntry(row, parent, to[len(to)-1]); err != nil {
		return nil, err
	}

	if err = c.checkLimits(before); err != nil {
		return nil, err
	}

	return c.fillEntry(row)
}

// isBeneath reports whether the path to is the path from or lies beneath it.
func isBeneath(from, to []string) bool {
	return len(to) >= len(from) && slices.Equal(from, to[:len(from)])
}

// relinkEntry gives an entry a new parent and name. Both parents get the same node time and lose their cached IDs.
func (c *change) relinkEntry(row, parent *dbEntry, name string) error {
	_, err := c.tx.findChild(parent.ID, name)
	switch {
	case err == nil:
		return vfsapi.ErrAlreadyExists
	case !errors.Is(err, vfsapi.ErrNotFound):
		return err
	}

	oldParentID := row.ParentID
	row.ParentID, row.Name = parent.ID, name

	if err = c.tx.setEntryParent(row.ID, row.ParentID, row.Name); err != nil {
		return err
	}

	now := nodeTime()
	for _, folderID := range []int64{oldParentID, parent.ID} {
		if err = c.markFolderChanged(folderID, now); err != nil {
			return err
		}
	}

	return nil
}

// removeEntry removes the entry the names give, with everything below it when recursive is set.
func (c *change) removeEntry(names []string, recursive bool) error {
	// why: the root cannot be removed (operator decision 2026-11-12).
	if len(names) == 0 {
		return vfsapi.ErrInvalidPath
	}

	row, err := c.tx.resolveEntry(c.vol, names)
	if err != nil {
		return err
	}

	if row.isFolder() && !recursive {
		nonEmpty, err := c.tx.hasChildren(row.ID)
		if err != nil {
			return err
		}
		if nonEmpty {
			return vfsapi.ErrFolderNotEmpty
		}
	}

	if err = c.tx.deleteSubtree(row.ID); err != nil {
		return err
	}

	return c.markFolderChanged(row.ParentID, nodeTime())
}

// touchEntry sets the time of the entry the names give.
//
// note: the time of a folder is outside its own directory, so only the folders above it lose their cached IDs.
func (c *change) touchEntry(names []string, modTime int64) (*vfsapi.Entry, error) {
	// why: the root cannot be touched (operator decision 2026-11-12).
	if len(names) == 0 {
		return nil, vfsapi.ErrInvalidPath
	}

	row, err := c.tx.resolveEntry(c.vol, names)
	if err != nil {
		return nil, err
	}

	if err = c.tx.setEntryTime(row.ID, modTime); err != nil {
		return nil, err
	}
	row.ModTime = modTime

	if err = c.tx.clearFolderIDs(row.ParentID); err != nil {
		return nil, err
	}

	return c.fillEntry(row)
}

// encodableTime reports whether the time type encodes a time: whole nanoseconds since the Unix epoch in a uint64,
// within the int64 range that time.Time converts without loss.
func encodableTime(t time.Time) bool {
	ns := t.UnixNano()

	return ns >= 0 && time.Unix(0, ns).Equal(t)
}
