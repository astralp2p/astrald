package vfs

import (
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

// todo: implement vfs.move; the stub answers an error until a later part of the VFS ticket fills this file.
// why: the stub reads and changes nothing, so it asks for no named action; the part that fills it authorizes first.
func (mod *Module) OpMove(ctx *astral.Context, q *routing.IncomingQuery, args opMoveArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Send(errNotImplemented)
}
