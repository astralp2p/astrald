package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opReadMessagesArgs struct {
	In  string
	Out string
}

// OpReadMessages reads whole messages from the caller's own mail, with their
// direct replies. The request arrives after the query is accepted, as one
// messaging.read_messages_request.
func (mod *Module) OpReadMessages(ctx *astral.Context, q *routing.IncomingQuery, args opReadMessagesArgs) error {
	if !mod.admitsMailCaller(q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var req *messaging.ReadMessagesRequest
	if err := ch.Switch(channel.Expect(&req)); err != nil {
		return ch.Send(astral.Err(err))
	}

	res, err := mod.ReadMessages(ctx, q.Caller(), req)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(res)
}
