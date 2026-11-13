package vfs

import (
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

	if err := mod.authorizeManageQuery(ctx, q); err != nil {
		return ch.Send(astral.Err(err))
	}

	volumes, err := mod.db.listVolumes()
	if err != nil {
		mod.log.Error("vfs.volumes: list volumes: %v", err)
		return ch.Send(astral.Err(err))
	}

	for _, vol := range volumes {
		info, err := mod.vfsInfo(vol)
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
