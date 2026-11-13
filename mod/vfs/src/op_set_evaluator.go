package vfs

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSetEvaluatorArgs struct {
	In       string
	Out      string
	VFS      *astral.Identity `query:"required"`
	Endpoint string
}

// OpSetEvaluator names the evaluator of a VFS and answers ack. An empty endpoint clears it. An endpoint that is
// not astral://<identity-or-alias>:vfs.authorize answers invalid endpoint.
func (mod *Module) OpSetEvaluator(ctx *astral.Context, q *routing.IncomingQuery, args opSetEvaluatorArgs) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	if err := mod.authorizeEvaluatorQuery(ctx, q, args.VFS); err != nil {
		return ch.Send(astral.Err(err))
	}

	if err := mod.SetEvaluator(args.VFS, args.Endpoint); err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&astral.Ack{})
}
