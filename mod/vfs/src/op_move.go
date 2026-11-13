package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMoveArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	From string `query:"required"`
	To   string `query:"required"`
}

// OpMove moves or renames an entry and answers the moved entry. Missing parents of the target are created, the
// entry keeps its time, and both parent folders get the node's time. See VFS 6, section 7.
func (mod *Module) OpMove(ctx *astral.Context, q *routing.IncomingQuery, args opMoveArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessWrite)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	from, err := parsePath(args.From)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	to, err := parsePath(args.To)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	var entry *vfsapi.Entry

	err = mod.changeVFS(vol, func(c *change) (err error) {
		entry, err = c.moveEntry(from, to)
		return err
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(entry)
}
