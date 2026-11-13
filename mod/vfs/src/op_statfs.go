package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opStatfsArgs struct {
	In  string
	Out string
	VFS *astral.Identity
}

// OpStatfs answers the facts about one VFS (VFS 6, section 7).
func (mod *Module) OpStatfs(ctx *astral.Context, q *routing.IncomingQuery, args opStatfsArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.readTarget(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	info, err := mod.vfsInfo(vol)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(info)
}

// vfsInfo returns the facts about a VFS for vfs.statfs and vfs.volumes. A limit of 0 means no limit. The root is
// not an entry.
//
// why: Root, Usage and Entries are read in one transaction under the VFS lock, so they describe one state.
// note: a caller must not hold the lock of the VFS, nor be inside a transaction.
func (mod *Module) vfsInfo(vol *dbVolume) (*vfsapi.Info, error) {
	info := &vfsapi.Info{
		VFS:        vol.Identity,
		Quota:      astral.Uint64(vol.byteLimit()),
		EntryLimit: astral.Uint64(vol.entryLimit()),
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

		info.Root, info.Usage, info.Entries = id, astral.Uint64(u.Bytes), astral.Uint64(u.Entries)

		return tx.setFolderIDs(fc.filled)
	})
	if err != nil {
		return nil, err
	}

	return info, nil
}
