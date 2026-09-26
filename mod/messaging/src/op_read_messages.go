package messaging

import (
	"errors"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

type opReadMessagesArgs struct {
	In  string
	Out string
}

// errReadRefused is what readFor answers for a delegated reader the read
// refuses: the operation then ends the query with no answer.
var errReadRefused = errors.New("read refused")

// OpReadMessages reads whole messages from a mailbox, with their direct
// replies: the caller's own, or the one the request's Mailbox names as a
// delegated read. The request arrives after the query is accepted, as one
// messaging.read_messages_request.
//
// why only the origin and the caller's identity are checked before the query
// is accepted: the request names the mailbox read and arrives once the query
// is accepted, and a delegated reader need not have a mailbox here.
//
// why a delegated reader refused after the request ends the query with no
// answer: the refusal of a delegated read carries no bytes, as a rejected query
// carries none.
//
// why a caller whose own mailbox this node does not host is answered
// errNotParticipant: the mailbox read is the caller's own, so the answer tells
// no other identity anything, and a caller told its mailbox is not hosted
// tells that apart from a node that went silent.
func (mod *Module) OpReadMessages(ctx *astral.Context, q *routing.IncomingQuery, args opReadMessagesArgs) error {
	if refusesOrigin(q) || !mod.mayRead(q.Caller()) {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithFormats(args.In, args.Out))
	defer ch.Close()

	var req *messaging.ReadMessagesRequest
	if err := ch.Switch(channel.Expect(&req)); err != nil {
		return ch.Send(astral.Err(err))
	}

	res, err := mod.readFor(ctx, q.Caller(), req)
	switch {
	case errors.Is(err, errReadRefused):
		return nil
	case err != nil:
		return ch.Send(astral.Err(err))
	}

	return ch.Send(res)
}

// readFor answers the read the caller asked for. With no mailbox, or the
// caller's, the caller reads its own mailbox through ReadMessages, which
// answers errNotParticipant where this node does not host it. Any other
// mailbox is a delegated read — see admitsReader. A delegated reader the read
// refuses is answered errReadRefused.
func (mod *Module) readFor(ctx *astral.Context, caller *astral.Identity, req *messaging.ReadMessagesRequest) (*messaging.ReadMessagesResult, error) {
	mailbox := mailboxOf(req)

	if mailbox == nil || mailbox.IsEqual(caller) {
		return mod.ReadMessages(ctx, caller, req)
	}

	if !mod.admitsReader(ctx, caller, mailbox) {
		return nil, errReadRefused
	}
	return mod.read(mailbox, req, true)
}
