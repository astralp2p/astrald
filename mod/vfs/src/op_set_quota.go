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

// OpSetQuota sets the byte limit and the entry limit of a VFS and answers ack. A limit of 0 means no limit.
//
// A limit below the current usage is accepted. It refuses further growth and removes nothing.
func (mod *Module) OpSetQuota(ctx *astral.Context, q *routing.IncomingQuery, args opSetQuotaArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err := mod.manageAuthorize(ctx, q); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.setLimits(args.VFS, args.Bytes, args.Entries); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
