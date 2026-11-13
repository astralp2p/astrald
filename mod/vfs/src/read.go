package vfs

import (
	"io"

	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// readRepository is the repository group that holds VFS content.
const readRepository = "local"

// readEntry returns the entry of a row as it is answered. The ID of a folder is its directory ObjectID, computed
// from the folders below it when it is cleared.
//
// note: a caller must not hold the lock of the VFS, nor be inside a transaction; the computation takes both.
func (mod *Module) readEntry(vol *dbVolume, row *dbEntry) (*vfsapi.Entry, error) {
	if row.isFolder() {
		id, err := mod.folderObjectID(vol.Identity, row.ID)
		if err != nil {
			return nil, err
		}
		row.ObjectID = &id
	}

	return row.toEntry(), nil
}

// listFolder returns the children of the folder at a path, in one read transaction.
func (mod *Module) listFolder(vol *dbVolume, names []string) (rows []*dbEntry, err error) {
	err = mod.db.inTx(func(tx *DB) error {
		folder, err := tx.resolveEntry(vol, names)
		if err != nil {
			return err
		}
		if !folder.isFolder() {
			return vfsapi.ErrNotAFolder
		}

		rows, err = tx.listChildren(folder.ID)

		return err
	})

	return rows, err
}

// findFileID returns the ObjectID of the content of the file at a path. A path that names a folder fails.
func (mod *Module) findFileID(vol *dbVolume, names []string) (id astral.ObjectID, err error) {
	row, err := mod.db.resolveEntry(vol, names)
	if err != nil {
		return id, err
	}
	if row.isFolder() {
		return id, vfsapi.ErrIsAFolder
	}
	if row.ObjectID == nil {
		return id, vfsapi.ErrNotFound
	}

	return *row.ObjectID, nil
}

// answerRaw accepts the query and copies the content to the caller.
func answerRaw(q *routing.IncomingQuery, content io.Reader) error {
	conn := q.AcceptRaw()
	defer conn.Close()

	if content == nil {
		return nil
	}

	_, err := io.Copy(conn, content)

	return err
}

// vfsInfo returns the facts about a VFS for vfs.statfs and vfs.volumes. A limit of 0 means no limit. The root is
// not an entry.
//
// why: Root, Usage and Inodes are read in one transaction under the VFS lock, so they describe one state.
// note: a caller must not hold the lock of the VFS, nor be inside a transaction.
func (mod *Module) vfsInfo(vol *dbVolume) (*vfsapi.Info, error) {
	info := &vfsapi.Info{
		VFS:        vol.Identity,
		Quota:      astral.Uint64(vol.byteLimit()),
		InodeLimit: astral.Uint64(vol.inodeLimit()),
	}

	err := mod.runLockedTx(vol.Identity, func(tx *DB) error {
		root, err := tx.findEntry(vol.RootID)
		if err != nil {
			return err
		}

		fc := folderIDComputer{tx: tx}

		id, err := fc.compute(root)
		if err != nil {
			return err
		}

		u, err := tx.measureUsage(vol.Identity)
		if err != nil {
			return err
		}

		info.Root, info.Usage, info.Inodes = id, astral.Uint64(u.Bytes), astral.Uint64(u.Inodes)

		return tx.setFolderIDs(fc.filled)
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}
