package messaging

import (
	"errors"
	"fmt"
	"math"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/lib/routing"
)

// The paging operations read a mailbox a bounded page at a time. Each answers
// one object and an eos, or an error_message: a caller applies nothing it read
// before the eos. Admission is list_messages': the caller's own mailbox, or a
// delegated read of one this node hosts.

type opPageMessagesArgs struct {
	List       string
	Peer       string
	Before     uint64
	Limit      uint64
	Generation *uint64
	Mailbox    string
	Out        string
}

// OpPageMessages answers one page newest first: one list, or one peer's
// unarchived rows in both boxes.
func (mod *Module) OpPageMessages(ctx *astral.Context, q *routing.IncomingQuery, args opPageMessagesArgs) error {
	return mod.answerPage(ctx, q, args.Mailbox, args.Out, func(mailbox *astral.Identity) (astral.Object, error) {
		pq, err := mod.pageQueryOf(args)
		if err != nil {
			return nil, err
		}
		page, err := mod.db.PageMessages(mailbox, pq)
		if err != nil {
			return nil, err
		}
		if err = sameGeneration(args.Generation, args.Before != 0, page.Generation); err != nil {
			return nil, err
		}
		return &messaging.MessagePage{
			Messages:   listed(page.Rows),
			NextBefore: astral.Uint64(page.Next),
			Rev:        astral.Uint64(page.Rev),
			Generation: astral.Uint64(page.Generation),
		}, nil
	})
}

type opListMessageChangesArgs struct {
	Peer       string
	Since      uint64
	Limit      uint64
	Generation *uint64
	Mailbox    string
	Out        string
}

// OpListMessageChanges answers the rows written or changed after since, oldest
// change first, archived rows included.
func (mod *Module) OpListMessageChanges(ctx *astral.Context, q *routing.IncomingQuery, args opListMessageChangesArgs) error {
	return mod.answerPage(ctx, q, args.Mailbox, args.Out, func(mailbox *astral.Identity) (astral.Object, error) {
		since, err := positionOf("since", args.Since)
		if err != nil {
			return nil, err
		}
		limit, err := limitOf(args.Limit)
		if err != nil {
			return nil, err
		}
		peer, err := mod.peerOf(args.Peer)
		if err != nil {
			return nil, err
		}
		page, err := mod.db.MessageChanges(mailbox, peer, since, limit)
		if err != nil {
			return nil, err
		}
		if err = sameGeneration(args.Generation, true, page.Generation); err != nil {
			return nil, err
		}
		return &messaging.MessageChanges{
			Messages:   listed(page.Rows),
			NextRev:    astral.Uint64(page.Next),
			More:       astral.Bool(page.More),
			Generation: astral.Uint64(page.Generation),
		}, nil
	})
}

type opPageConversationsArgs struct {
	Peer       string
	Before     uint64
	Limit      uint64
	Generation *uint64
	Mailbox    string
	Out        string
}

// OpPageConversations answers one page of conversations by latest message,
// newest first, or the one conversation with a peer.
func (mod *Module) OpPageConversations(ctx *astral.Context, q *routing.IncomingQuery, args opPageConversationsArgs) error {
	return mod.answerPage(ctx, q, args.Mailbox, args.Out, func(mailbox *astral.Identity) (astral.Object, error) {
		if args.Peer != "" && args.Before != 0 {
			return nil, errors.New("peer names one conversation; before pages them all")
		}
		before, err := positionOf("before", args.Before)
		if err != nil {
			return nil, err
		}
		limit, err := limitOf(args.Limit)
		if err != nil {
			return nil, err
		}
		peer, err := mod.peerOf(args.Peer)
		if err != nil {
			return nil, err
		}
		page, err := mod.db.PageConversations(mailbox, peer, before, limit)
		if err != nil {
			return nil, err
		}
		if err = sameGeneration(args.Generation, before != 0, page.Generation); err != nil {
			return nil, err
		}
		return &messaging.ConversationPage{
			Conversations: conversations(page.Conversations),
			NextBefore:    astral.Uint64(page.Next),
			Rev:           astral.Uint64(page.Rev),
			Generation:    astral.Uint64(page.Generation),
		}, nil
	})
}

type opListConversationChangesArgs struct {
	Since      uint64
	Limit      uint64
	Generation *uint64
	Mailbox    string
	Out        string
}

// OpListConversationChanges answers the conversations that changed after
// since, oldest change first, tombstones included.
func (mod *Module) OpListConversationChanges(ctx *astral.Context, q *routing.IncomingQuery, args opListConversationChangesArgs) error {
	return mod.answerPage(ctx, q, args.Mailbox, args.Out, func(mailbox *astral.Identity) (astral.Object, error) {
		since, err := positionOf("since", args.Since)
		if err != nil {
			return nil, err
		}
		limit, err := limitOf(args.Limit)
		if err != nil {
			return nil, err
		}
		page, err := mod.db.ConversationChanges(mailbox, since, limit)
		if err != nil {
			return nil, err
		}
		if err = sameGeneration(args.Generation, true, page.Generation); err != nil {
			return nil, err
		}
		return &messaging.ConversationChanges{
			Conversations: conversations(page.Conversations),
			NextRev:       astral.Uint64(page.Next),
			More:          astral.Bool(page.More),
			Generation:    astral.Uint64(page.Generation),
		}, nil
	})
}

// answerPage admits the query as a listing, reads the one answer, and sends it
// with its eos, or the error in its place.
//
// why the hosting check runs again after acceptance: list_messages makes it
// there too, so a mailbox withdrawn between the two answers the same error on
// every listing.
func (mod *Module) answerPage(ctx *astral.Context, q *routing.IncomingQuery, name, out string, read func(*astral.Identity) (astral.Object, error)) error {
	mailbox, ok := mod.admitsListing(ctx, q, name)
	if !ok {
		return q.Reject()
	}

	ch := channel.New(q.AcceptRaw(), channel.WithOutputFormat(out))
	defer ch.Close()

	if !mod.hosts(mailbox) {
		return ch.Send(astral.Err(errNotParticipant))
	}
	answer, err := read(mailbox)
	if err != nil {
		return ch.Send(astral.Err(err))
	}
	if err = ch.Send(answer); err != nil {
		return err
	}
	return ch.Send(&astral.EOS{})
}

// pageQueryOf checks a page request and turns it into the store's words.
func (mod *Module) pageQueryOf(args opPageMessagesArgs) (q pageQuery, err error) {
	if args.List != "" && args.Peer != "" {
		return q, errors.New("list and peer are exclusive: peer reads both boxes")
	}
	switch args.List {
	case "", messaging.ListInbox, messaging.ListOutbox, messaging.ListArchive:
		q.List = args.List
	default:
		return q, fmt.Errorf("no such list: %v", args.List)
	}
	if q.Before, err = positionOf("before", args.Before); err != nil {
		return q, err
	}
	if q.Limit, err = limitOf(args.Limit); err != nil {
		return q, err
	}
	q.Peer, err = mod.peerOf(args.Peer)
	return q, err
}

// peerOf resolves a correspondent, or answers nil for none named.
func (mod *Module) peerOf(name string) (*astral.Identity, error) {
	if name == "" {
		return nil, nil
	}
	peer, err := mod.Dir.ResolveIdentity(name)
	if err != nil {
		return nil, errUnknownPeer(name)
	}
	return peer, nil
}

// positionOf refuses a position no answer could have given: the store's
// positions are signed 64-bit integers.
func positionOf(name string, v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%v is a position a previous answer gave you, not %v", name, v)
	}
	return int64(v), nil
}

// limitOf answers the page size: zero takes the default, and one over the
// ceiling is refused rather than clamped, so a caller never mistakes a short
// page for the end.
func limitOf(v uint64) (int, error) {
	switch {
	case v == 0:
		return messaging.PageLimitDefault, nil
	case v > messaging.PageLimitMax:
		return 0, fmt.Errorf("limit is at most %v, not %v", messaging.PageLimitMax, v)
	}
	return int(v), nil
}

// sameGeneration checks that a position names the mailbox's rows as they stand.
// A request holding a position must carry the generation it came with; one that
// starts a traversal may carry none.
//
// why after the read, in its transaction: the generation and the rows then
// describe one state, so a deletion cannot fall between the check and the read.
func sameGeneration(asked *uint64, required bool, have int64) error {
	switch {
	case asked == nil && required:
		return errors.New("generation is required with a position")
	case asked != nil && *asked != uint64(have):
		return errors.New(messaging.ErrGeneration)
	}
	return nil
}

// listed renders rows as page entries, without bodies.
func listed(rows []dbMessage) []*messaging.ListedMessage {
	list := make([]*messaging.ListedMessage, len(rows))
	for i, row := range rows {
		list[i] = &messaging.ListedMessage{Rev: astral.Uint64(row.Rev), Envelope: row.stored().Envelope()}
	}
	return list
}

// conversations renders summaries, a tombstone without a latest envelope.
func conversations(rows []dbConversation) []*messaging.Conversation {
	list := make([]*messaging.Conversation, len(rows))
	for i, c := range rows {
		list[i] = &messaging.Conversation{Peer: c.Peer, Unread: astral.Uint64(c.Unread), Rev: astral.Uint64(c.Rev)}
		if c.Latest != nil {
			list[i].Latest = c.Latest.stored().Envelope()
		}
	}
	return list
}
