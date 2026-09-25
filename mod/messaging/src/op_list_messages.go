package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opListMessagesArgs struct {
	List           string
	From           string
	To             string
	Since          uint64
	UnreadOnly     bool
	AwaitingPickup bool
	Out            string
}

// OpListMessages streams one of the caller's lists as envelopes, without
// bodies, and ends the stream with eos.
func (mod *Module) OpListMessages(ctx *astral.Context, q *routing.IncomingQuery, args opListMessagesArgs) error {
	if !mod.admitsMailCaller(q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	list, err := mod.ListMessages(ctx, q.Caller(), messaging.ListMessagesRequest{
		List:           args.List,
		From:           args.From,
		To:             args.To,
		Since:          args.Since,
		UnreadOnly:     args.UnreadOnly,
		AwaitingPickup: args.AwaitingPickup,
	})
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	for _, m := range list {
		if err = ch.Send(m); err != nil {
			return err
		}
	}

	return ch.Send(&astral.EOS{})
}
