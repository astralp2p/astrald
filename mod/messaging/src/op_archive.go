package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opArchiveArgs struct {
	Box  string              `query:"required"`
	ID   messaging.MessageID `query:"required"`
	Undo bool
	Out  string
}

// OpArchive puts one of the caller's messages away, or back with undo, and
// answers whether this call moved it.
func (mod *Module) OpArchive(ctx *astral.Context, q *routing.IncomingQuery, args opArchiveArgs) error {
	if !mod.admitsMailCaller(q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	ref := messaging.MessageRef{Box: astral.String8(args.Box), ID: args.ID}

	changed, err := mod.Archive(ctx, q.Caller(), ref, args.Undo)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&messaging.ArchiveResult{Changed: astral.Bool(changed)})
}
