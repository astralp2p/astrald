package vfs

import (
	"errors"
	"math"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// computeAllowance checks the target of a write and returns how many bytes the stream may carry: the bytes left
// under the byte limit plus the size of the file being replaced.
func (mod *Module) computeAllowance(vol *dbVolume, names []string) (uint64, error) {
	replaced, err := mod.findReplacedFile(vol, names)
	if err != nil {
		return 0, err
	}

	limit := vol.byteLimit()
	if limit == 0 {
		return math.MaxUint64, nil
	}

	usage, err := mod.db.usage(vol.Identity)
	if err != nil {
		return 0, err
	}

	// note: a coarse bound; the exact check runs in the transaction after the commit.
	allowed := limit - min(usage, limit)
	if replaced != nil && replaced.ObjectID != nil {
		allowed += replaced.ObjectID.Size
	}

	return allowed, nil
}

// findReplacedFile returns the file a write would replace, or nil when the path names no entry yet. A folder at the
// path answers vfsapi.ErrIsAFolder, a file on the way vfsapi.ErrNotAFolder.
func (mod *Module) findReplacedFile(vol *dbVolume, names []string) (*dbEntry, error) {
	row, err := mod.db.resolveEntry(vol, names)
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	case row.isFolder():
		return nil, vfsapi.ErrIsAFolder
	}

	return row, nil
}

// openWriter opens a writer in the default write repository, the group local.
func (mod *Module) openWriter(ctx *astral.Context) (objectsapi.Writer, error) {
	repo := mod.Objects.WriteDefault()
	if repo == nil {
		return nil, vfsapi.ErrNoStorage
	}

	w, err := repo.Create(ctx, nil)
	if err != nil {
		mod.log.Logv(1, "open writer in %v: %v", repo.Label(), err)
		return nil, vfsapi.ErrNoStorage
	}

	return w, nil
}

// receiveContent copies blobs into the writer until the commit message and commits. It answers a nil ObjectID when
// the stream ends before the commit message. A stream above the allowance is cut off with vfsapi.ErrQuotaExceeded.
func receiveContent(ctx *astral.Context, ch *channel.Channel, w objectsapi.Writer, allowed uint64) (id *astral.ObjectID, err error) {
	var size uint64

	err = ch.Switch(
		func(blob *astral.Blob) error {
			// why: a cut-off stream answers quota exceeded and its writer is discarded (operator decision 2026-11-12).
			if size += uint64(len(*blob)); size > allowed {
				return vfsapi.ErrQuotaExceeded
			}

			_, err := w.Write(*blob)
			return err
		},
		func(*objectsapi.CommitMsg) (err error) {
			if id, err = commitContent(w, size); err != nil {
				return err
			}

			return channel.ErrBreak
		},
		channel.WithContext(ctx),
	)

	return id, err
}

// commitContent commits the content and returns its ObjectID.
//
// why: the empty object is never stored (operator decision 2026-11-12); an empty file commits nothing and its ID is
// the ObjectID of empty content.
func commitContent(w objectsapi.Writer, size uint64) (*astral.ObjectID, error) {
	if size == 0 {
		return objectsmod.EmptyObjectID(), w.Discard()
	}

	return w.Commit()
}

// recordFile records the committed content at the path in one change: missing parents are created, a file there
// is replaced, and both limits are checked exactly.
func (mod *Module) recordFile(vol *dbVolume, names []string, id astral.ObjectID) (row *dbEntry, err error) {
	err = mod.changeVFS(vol, func(c *change) error {
		before, err := c.tx.measureUsage(c.vol.Identity)
		if err != nil {
			return err
		}

		parent, err := c.makeFolders(names[:len(names)-1])
		if err != nil {
			return err
		}

		if row, err = c.putFile(parent, names[len(names)-1], id); err != nil {
			return err
		}

		return c.checkLimits(before)
	})

	return row, err
}

// putFile inserts a file into a folder or replaces the content of the file there.
//
// note: replacing content sets the file's time and leaves the folder's time alone (VFS 6, section 7).
func (c *change) putFile(parent *dbEntry, name string, id astral.ObjectID) (*dbEntry, error) {
	row, err := c.tx.findChild(parent.ID, name)
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		row = &dbEntry{Name: name, ObjectID: &id}
		return row, c.addEntry(parent, row)
	case err != nil:
		return nil, err
	case row.isFolder():
		return nil, vfsapi.ErrIsAFolder
	}

	row.ObjectID, row.ModTime = &id, nodeTime()

	if err = c.tx.setFileContent(row.ID, row.ObjectID, row.ModTime); err != nil {
		return nil, err
	}

	return row, c.tx.clearFolderIDs(parent.ID)
}
