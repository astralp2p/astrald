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
	Mailbox        string
	Out            string
}

// OpListMessages streams one list of a mailbox as envelopes, without bodies,
// and ends the stream with eos. The mailbox is the caller's own, or the one
// Mailbox names as a delegated read.
func (mod *Module) OpListMessages(ctx *astral.Context, q *routing.IncomingQuery, args opListMessagesArgs) error {
	mailbox, ok := mod.admitsListing(ctx, q, args.Mailbox)
	if !ok {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(args.Out))
	defer ch.Close()

	// why listMailbox and not ListMessages: the mailbox is resolved and
	// admitted above, and ListMessages lists through listMailbox as well, so a
	// tool and this operation share one hosting check.
	list, err := mod.listMailbox(mailbox, messaging.ListMessagesRequest{
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

// admitsListing answers the mailbox a listing reads, and whether the query is
// served. A query from an origin this module does not serve is refused. With
// no name, or a name for the caller, the mailbox is the caller's own and the
// caller must be hosted here, as for every mail operation. Any other name makes
// the listing a delegated read — see admitsReader.
//
// why a name that resolves to nobody is refused and not answered an error: a
// mailbox this node cannot name is one it does not host, and the refusal of a
// delegated read carries no bytes.
func (mod *Module) admitsListing(ctx *astral.Context, q *routing.IncomingQuery, name string) (*astral.Identity, bool) {
	caller := q.Caller()
	if refusesOrigin(q) {
		return nil, false
	}
	if name == "" {
		return caller, mod.hosts(caller)
	}

	mailbox, err := mod.Dir.ResolveIdentity(name)
	switch {
	case err != nil:
		return nil, false
	case mailbox.IsEqual(caller):
		return caller, mod.hosts(caller)
	}

	return mailbox, mod.admitsReader(ctx, caller, mailbox)
}
