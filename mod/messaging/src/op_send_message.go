package messaging

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opSendMessageArgs struct {
	In  string
	Out string
}

// OpSendMessage sends one message from the caller. The request arrives after
// the query is accepted, as one messaging.send_message_request, and the answer
// is the id the message is stored under.
func (mod *Module) OpSendMessage(ctx *astral.Context, q *routing.IncomingQuery, args opSendMessageArgs) error {
	if !mod.admitsMailCaller(q) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var req *messaging.SendMessageRequest
	if err := ch.Switch(channel.Expect(&req)); err != nil {
		return ch.Send(astral.Err(err))
	}

	id, err := mod.SendMessage(ctx, q.Caller(), req)
	if err != nil {
		return ch.Send(astral.Err(err))
	}

	return ch.Send(&id)
}
