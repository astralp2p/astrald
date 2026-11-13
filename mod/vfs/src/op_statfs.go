package vfs

import (
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

	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessRead)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	info, err := mod.vfsInfo(vol)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(info)
}
