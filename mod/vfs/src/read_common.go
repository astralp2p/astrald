package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// readTarget authorizes the caller of a read operation on the VFS the operation names. An operation without a
// vfs argument names the caller.
//
// The caller checks the path after this call, as VFS 6, section 6 orders it.
func (mod *Module) readTarget(ctx *astral.Context, q *routing.IncomingQuery, vfsArg *astral.Identity) (*dbVolume, error) {
	if vfsArg == nil {
		vfsArg = q.Caller()
	}

	return mod.authorizeVFS(ctx, q.Caller(), vfsArg, accessRead)
}

// readEntry returns the entry of a row as it is answered. The ID of a folder is its listing ObjectID, computed
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
