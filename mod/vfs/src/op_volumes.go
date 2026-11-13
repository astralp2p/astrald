package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opVolumesArgs struct {
	In  string
	Out string
}

// OpVolumes streams the facts of every VFS of the node as mod.vfs.info, then eos.
func (mod *Module) OpVolumes(ctx *astral.Context, q *routing.IncomingQuery, args opVolumesArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err := mod.manageAuthorize(ctx, q); err != nil {
		return ch.Send(astral.Err(err))
	}

	volumes, err := mod.db.volumesList()
	if err != nil {
		mod.log.Error("vfs.volumes: list volumes: %v", err)
		return ch.Send(astral.Err(err))
	}

	for _, vol := range volumes {
		info, err := mod.volumesInfo(vol)
		if err != nil {
			mod.log.Error("vfs.volumes: info of %v: %v", vol.Identity, err)
			return ch.Send(astral.Err(err))
		}

		if err = ch.Send(info); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}

// volumesList returns every VFS in the order of creation.
func (db *DB) volumesList() (volumes []*dbVolume, err error) {
	err = db.Order("id").Find(&volumes).Error

	return
}

// volumesInfo returns the facts of one VFS.
//
// note: the facts are read one after another and not in one transaction, because the root ObjectID takes the
// VFS lock when it is not cached and the lock is never held across a read of this kind.
func (mod *Module) volumesInfo(vol *dbVolume) (*vfsapi.Info, error) {
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
