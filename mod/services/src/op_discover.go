package services

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/lib/routing"
)

type opDiscoverArgs struct {
	Follow bool `query:"optional"`

	In  string `query:"optional"`
	Out string `query:"optional"`
}

// OpDiscover handles the discover query: streams service updates to the caller,
// sending astral.EOS as a snapshot/stream separator when follow is true, and a
// final astral.EOS when the stream ends.
func (mod *Module) OpDiscover(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	args opDiscoverArgs,
) error {
	ch := q.Accept(channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	updates, err := mod.DiscoverServices(ctx, q.Caller(), args.Follow)
	if err != nil {
		return ch.Send(astral.NewError(err.Error()))
	}

	for update := range updates {
		if update == nil {
			err = ch.Send(&astral.EOS{})
		} else {
			err = ch.Send(update)
		}
		if err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
