package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opMountRemoteArgs struct {
	Path     string `query:"required"`
	Identity string `query:"required"`
	Root     string
	In       string
	Out      string
}

func (mod *Module) OpMountRemote(ctx *astral.Context, q *routing.IncomingQuery, args opMountRemoteArgs) (err error) {
	// why: the check sits at the op and not in Module.MountRemote, because MountRemote
	// also serves in-process callers, which carry no query caller to authorize.
	// why: a refused caller must reach neither the directory nor the remote tree.
	if !mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{Action: auth.NewAction(q.Caller())}) {
		return q.Reject()
	}

	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	targetID, err := mod.Dir.ResolveIdentity(args.Identity)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.MountRemote(ctx, args.Path, targetID, args.Root); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
