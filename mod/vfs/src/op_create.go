package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opCreateArgs struct {
	In  string
	Out string
	VFS *astral.Identity `query:"required"`
}

// OpCreate creates the VFS of an identity with the default quotas and answers ack. A VFS that exists is left
// as it is. The zero identity answers not permitted.
func (mod *Module) OpCreate(ctx *astral.Context, q *routing.IncomingQuery, args opCreateArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err := mod.authorizeManageQuery(ctx, q); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.CreateVFS(args.VFS); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
