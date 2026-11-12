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

// todo: implement vfs.remove; the stub answers an error until a later part of the VFS ticket fills this file.
// why: the stub reads and changes nothing, so it asks for no named action; the part that fills it authorizes first.
func (mod *Module) OpRemove(ctx *astral.Context, q *routing.IncomingQuery, args opRemoveArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Send(errNotImplemented)
}
