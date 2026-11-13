package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opRemoveArgs struct {
	In        string
	Out       string
	VFS       *astral.Identity
	Path      string `query:"required"`
	Recursive bool
}

// OpRemove removes an entry and answers ack. A folder with entries needs recursive. The content of a removed file
// stays in the repository, unheld, until purge. See VFS 6, section 7.
func (mod *Module) OpRemove(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveArgs) error {
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

	err = mod.changeVFS(vol, func(c *change) error { return c.removeEntry(names, args.Recursive) })
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
