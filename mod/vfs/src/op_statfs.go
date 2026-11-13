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

	info, err := mod.statfsInfo(vol)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(info)
}

// statfsInfo returns the facts about a VFS. A limit of 0 means no limit. The root is not an entry.
//
// note: a caller must not hold the lock of the VFS, nor be inside a transaction.
func (mod *Module) statfsInfo(vol *dbVolume) (*vfsapi.Info, error) {
	root, err := mod.folderObjectID(vol.Identity, vol.RootID)
	if err != nil {
		return nil, err
	}

	usage, err := mod.db.usage(vol.Identity)
	if err != nil {
		return nil, err
	}

	entries, err := mod.db.entryCount(vol.Identity)
	if err != nil {
		return nil, err
	}

	return &vfsapi.Info{
		VFS:        vol.Identity,
		Root:       root,
		Usage:      astral.Uint64(usage),
		Quota:      astral.Uint64(vol.byteLimit()),
		Entries:    astral.Uint64(entries),
		EntryLimit: astral.Uint64(vol.entryLimit()),
	}, nil
}
