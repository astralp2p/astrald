package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opStatArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
}

// OpStat answers the entry at a path (VFS 6, section 7). For "/" the entry has an empty Name, the folder flag,
// the root ObjectID and the root's modification time.
func (mod *Module) OpStat(ctx *astral.Context, q *routing.IncomingQuery, args opStatArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.readTarget(ctx, q, args.VFS)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	row, err := mod.db.resolveEntry(vol, names)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	entry, err := mod.readEntry(vol, row)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(entry)
}
