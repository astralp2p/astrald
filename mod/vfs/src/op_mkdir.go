package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMkdirArgs struct {
	In   string
	Out  string
	VFS  *astral.Identity
	Path string `query:"required"`
}

// OpMkdir creates a folder and its missing parents, and answers ack. An existing folder succeeds; a file at the
// path answers vfsapi.ErrAlreadyExists. See VFS 6, section 7.
func (mod *Module) OpMkdir(ctx *astral.Context, q *routing.IncomingQuery, args opMkdirArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	vol, err := mod.authorizeVFSQuery(ctx, q, args.VFS, accessWrite)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	names, err := parsePath(args.Path)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	err = mod.changeVFS(vol, func(c *change) error { return c.createFolder(names) })
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
