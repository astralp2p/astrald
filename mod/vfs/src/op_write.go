package vfs

import (
	"errors"
	"math"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

type opWriteArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
}

// OpWrite creates or replaces a file. It answers ack when the writer is open, receives blobs until a
// mod.objects.commit_msg, commits the content and answers the entry. A stream that ends before the commit message
// changes nothing. See VFS 6, sections 7 and 10.
func (mod *Module) OpWrite(ctx *astral.Context, q *routing.IncomingQuery, args opWriteArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.changeAuthorize(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	// why: is a folder and not a folder are answered before the writer opens (operator decision 2026-11-12).
	allowed, err := mod.writeAllowance(vol, names)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	w, err := mod.writeOpen(ctx)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	defer w.Discard()

	if err = ch.Send(&astral.Ack{}); err != nil {
		return err
	}

	id, err := writeReceive(ctx, ch, w, allowed)
	switch {
	case err != nil:
		return ch.Send(astral.Err(err))
	case id == nil:
		// note: the stream ended before the commit message; nothing changes.
		return nil
	}

	row, err := mod.writeCommit(vol, names, *id)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(row.toEntry())
}

// writeAllowance checks the target of a write and returns how many bytes the stream may carry: the bytes left
// under the byte limit plus the size of the file being replaced.
func (mod *Module) writeAllowance(vol *dbVolume, names []string) (uint64, error) {
	replaced, err := mod.db.writeFindFile(vol, names)
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

// writeFindFile returns the file a write would replace, or nil when the path names no entry yet. A folder at the
// path answers vfsapi.ErrIsAFolder, a file on the way vfsapi.ErrNotAFolder.
func (db *DB) writeFindFile(vol *dbVolume, names []string) (*dbEntry, error) {
	row, err := db.resolveEntry(vol, names)
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

// writeOpen opens a writer in the default write repository, the group local.
func (mod *Module) writeOpen(ctx *astral.Context) (objectsapi.Writer, error) {
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

// writeReceive copies blobs into the writer until the commit message and commits. It answers a nil ObjectID when
// the stream ends before the commit message. A stream above the allowance is cut off with vfsapi.ErrQuotaExceeded.
func writeReceive(ctx *astral.Context, ch *channel.Channel, w objectsapi.Writer, allowed uint64) (id *astral.ObjectID, err error) {
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
			if id, err = writeFinish(w, size); err != nil {
				return err
			}

			return channel.ErrBreak
		},
		channel.WithContext(ctx),
	)

	return id, err
}

// writeFinish commits the content and returns its ObjectID.
//
// why: the empty object is never stored (operator decision 2026-11-12); an empty file commits nothing and its ID is
// the ObjectID of empty content.
func writeFinish(w objectsapi.Writer, size uint64) (*astral.ObjectID, error) {
	if size == 0 {
		return objectsmod.EmptyObjectID(), w.Discard()
	}

	return w.Commit()
}

// writeCommit records the committed content at the path in one change: missing parents are created, a file there
// is replaced, and both limits are checked exactly.
func (mod *Module) writeCommit(vol *dbVolume, names []string, id astral.ObjectID) (row *dbEntry, err error) {
	err = mod.changeVFS(vol, func(tx *DB) error {
		before, err := tx.measureUsage(vol.Identity)
		if err != nil {
			return err
		}

		parent, err := tx.changeMakeFolders(vol, names[:len(names)-1])
		if err != nil {
			return err
		}

		if row, err = tx.writeEntry(parent, names[len(names)-1], id); err != nil {
			return err
		}

		return tx.changeCheckLimits(vol.Identity, before)
	})

	return row, err
}

// writeEntry inserts a file into a folder or replaces the content of the file there.
//
// note: replacing content sets the file's time and leaves the folder's time alone (VFS 6, section 7).
func (db *DB) writeEntry(parent *dbEntry, name string, id astral.ObjectID) (*dbEntry, error) {
	row, err := db.findChild(parent.ID, name)
	switch {
	case errors.Is(err, vfsapi.ErrNotFound):
		row = &dbEntry{Name: name, ObjectID: &id}
		return row, db.changeAddEntry(parent, row)
	case err != nil:
		return nil, err
	case row.isFolder():
		return nil, vfsapi.ErrIsAFolder
	}

	row.ObjectID, row.ModTime = &id, nodeTime()

	if err = db.setFileContent(row.ID, row.ObjectID, row.ModTime); err != nil {
		return nil, err
	}

	return row, db.clearFolderIDs(parent.ID)
}
