package tree

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMountRemoteArgs struct {
	Path   string `query:"required"`
	Target string `query:"required"`
	Root   string
	In     string
	Out    string
}

func (mod *Module) OpMountRemote(ctx *astral.Context, q *routing.IncomingQuery, args opMountRemoteArgs) (err error) {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	targetID, err := mod.Dir.ResolveIdentity(args.Target)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.MountRemote(ctx, args.Path, targetID, args.Root); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
