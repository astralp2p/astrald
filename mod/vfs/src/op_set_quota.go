package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetQuotaArgs struct {
	In      string
	Out     string
	VFS     *astral.Identity `query:"required"`
	Bytes   uint64
	Entries uint64
}

// todo: implement vfs.set_quota; the stub answers an error until a later part of the VFS ticket fills this file.
// why: the stub reads and changes nothing, so it asks for no named action; the part that fills it authorizes first.
func (mod *Module) OpSetQuota(ctx *astral.Context, q *routing.IncomingQuery, args opSetQuotaArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	return ch.Send(errNotImplemented)
}
