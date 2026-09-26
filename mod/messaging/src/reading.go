package messaging

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// ReadMessages reads whole messages the owner holds, with their direct replies.
// The module's own bounds on one answer are messaging.MaxReadRefs and
// messaging.MaxChildren, and Config.MaxReadBytes bounds the bodies it carries.
//
// A request whose Mailbox names another identity is refused: the method reads
// the owner's own mailbox, and a delegated read is messaging.read_messages',
// which asks auth about its caller.
func (mod *Module) ReadMessages(_ context.Context, owner *astral.Identity, req *messaging.ReadMessagesRequest) (*messaging.ReadMessagesResult, error) {
	if mailbox := mailboxOf(req); mailbox != nil && !mailbox.IsEqual(owner) {
		return nil, errAnotherMailbox
	}
	if !mod.hosts(owner) {
		return nil, errNotParticipant
	}

	return mod.read(owner, req, false)
}

// read answers a read of a mailbox's messages. A delegated read hands out what
// it answers and records none of it — see handOut.
func (mod *Module) read(mailbox *astral.Identity, req *messaging.ReadMessagesRequest, delegated bool) (*messaging.ReadMessagesResult, error) {
	r, err := readRequestOf(req)
	if err != nil {
		return nil, err
	}
	r.Delegated = delegated

	res, err := mod.readMessages(mailbox, r)
	if err != nil {
		return nil, err
	}

	return res.result(), nil
}

// mailboxOf answers the mailbox a request names, nil when it names none.
func mailboxOf(req *messaging.ReadMessagesRequest) *astral.Identity {
	if req == nil {
		return nil
	}
	return req.Mailbox
}

// readRequestOf takes a request off the wire into the module's own words.
func readRequestOf(req *messaging.ReadMessagesRequest) (r readRequest, err error) {
	if req == nil {
		return r, errors.New("name at least one message to read")
	}

	r.Children = string(req.Children)
	r.MaxChildren = int(req.MaxChildren)
	for _, ref := range req.Refs {
		if ref == nil {
			return r, errors.New("a message ref names no message")
		}
		r.Refs = append(r.Refs, messageRef{Box: string(ref.Box), ID: ref.ID})
	}

	return r, nil
}

type readRequest struct {
	Refs        []messageRef
	Children    string
	MaxChildren int

	// Delegated says the reader is not the mailbox's identity: the read hands
	// bodies out and stamps nothing.
	Delegated bool
}

// validate refuses a read the module will not serve and fills in what the
// caller left out.
func (req *readRequest) validate() error {
	// why a repeat is dropped rather than refused: charging it twice would
	// spend a budget the caller cannot see. The bound is on distinct messages,
	// so the count is taken after the drop.
	req.Refs = distinctRefs(req.Refs)

	switch {
	case len(req.Refs) == 0:
		return errors.New("name at least one message to read")
	case len(req.Refs) > messaging.MaxReadRefs:
		return fmt.Errorf("name at most %v messages in one read", messaging.MaxReadRefs)
	}

	for _, ref := range req.Refs {
		if err := ref.validate(); err != nil {
			return err
		}
	}

	if req.Children == "" {
		req.Children = messaging.ChildrenEnvelopes
	}
	switch req.Children {
	case messaging.ChildrenNone, messaging.ChildrenEnvelopes, messaging.ChildrenFull:
	default:
		return fmt.Errorf("children is none, envelopes or full, not %v", req.Children)
	}

	if req.MaxChildren <= 0 {
		req.MaxChildren = messaging.MaxChildren
	}
	req.MaxChildren = min(req.MaxChildren, messaging.MaxChildren)

	return nil
}

// distinctRefs keeps the first of each named row, in the caller's order. The box
// is part of the identity: both rows of one id are two messages, not one twice.
func distinctRefs(refs []messageRef) []messageRef {
	seen := make(map[messageRef]bool, len(refs))
	out := refs[:0]

	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}

	return out
}

// readMessage is one message a read answers, with what the read decided about
// it.
type readMessage struct {
	Row *messaging.StoredMessage

	// ChildIDs are the ids of its direct replies, oldest first — the whole
	// set, whatever the answer carried of them.
	ChildIDs []messaging.MessageID

	// WithoutBody says the body is not part of this answer, and Truncated says
	// the reason was that the answer was already full rather than that
	// envelopes were asked for.
	WithoutBody bool
	Truncated   bool
}

type readResult struct {
	Messages []readMessage
	Replies  []readMessage
	NotFound []messageRef
}

// result renders what the read decided into the answer the operation sends.
func (res readResult) result() *messaging.ReadMessagesResult {
	out := &messaging.ReadMessagesResult{
		Messages: readMessagesOf(res.Messages),
		Replies:  readMessagesOf(res.Replies),
	}
	for _, ref := range res.NotFound {
		out.NotFound = append(out.NotFound, &messaging.MessageRef{Box: astral.String8(ref.Box), ID: ref.ID})
	}
	return out
}

// readMessagesOf renders each message with the body only where the read handed
// it out.
func readMessagesOf(list []readMessage) []*messaging.ReadMessage {
	out := make([]*messaging.ReadMessage, len(list))
	for i, m := range list {
		out[i] = &messaging.ReadMessage{
			Envelope:  m.Row.Envelope(),
			ChildIDs:  m.ChildIDs,
			Truncated: astral.Bool(m.Truncated),
		}
		if !m.WithoutBody {
			content := m.Row.Content
			out[i].Content = &content
		}
	}
	return out
}

// readMessages reads whole messages the owner holds, with their direct replies.
//
// why the replies are a flat set beside the messages: the edge is on the reply,
// which names its parent, and a nested answer refers to its own type — a shape
// the SDK's schema generator refuses.
func (mod *Module) readMessages(owner *astral.Identity, req readRequest) (res readResult, err error) {
	if err = req.validate(); err != nil {
		return res, err
	}

	rows, missing, err := mod.db.FindMany(owner, req.Refs)
	if err != nil {
		return res, err
	}

	// why every named message is handed out before any reply is read: a named
	// message may also be the reply of another, and its reply copy then reads
	// the stamp this read wrote.
	for _, row := range rows {
		if err = mod.handOut(owner, row, req.Delegated); err != nil {
			return res, err
		}
	}

	left := budget(mod.config.MaxReadBytes)

	for _, row := range rows {
		// why the message is charged before its replies: the caller named this
		// id and did not name the replies, so an overflow drops the extra
		// rather than the thing that was asked for.
		m := readMessage{Row: row}
		if !left.spend(len(row.Content)) {
			m.WithoutBody, m.Truncated = true, true
		}

		// why the ids come back whatever the children mode is: they are the
		// shape of the conversation, and the mode is about how much of the
		// replies' content this answer carries. A reader that asked for none
		// still needs to know what it could ask for next.
		if m.ChildIDs, err = mod.db.ChildIDs(owner, row.ID); err != nil {
			return res, err
		}

		if req.Children != messaging.ChildrenNone {
			var replies []readMessage
			if replies, err = mod.readReplies(owner, row.ID, req, &left); err != nil {
				return res, err
			}
			res.Replies = append(res.Replies, replies...)
		}

		res.Messages = append(res.Messages, m)
	}

	res.NotFound = missing

	return res, nil
}

// readReplies carries as much of one message's direct replies as the mode asks
// for. One level: the child ids on every message are what let a reader walk on.
//
// why a child's body is opt-in: handing one out to the mailbox's own reader
// stamps it read and tells its sender the body was collected, which a reader
// never asked for.
func (mod *Module) readReplies(owner *astral.Identity, parent messaging.MessageID, req readRequest, left *budget) (replies []readMessage, err error) {
	rows, err := mod.db.Children(owner, parent, req.MaxChildren)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if req.Children == messaging.ChildrenEnvelopes {
			replies = append(replies, readMessage{Row: row, WithoutBody: true})
			continue
		}

		if err = mod.handOut(owner, row, req.Delegated); err != nil {
			return nil, err
		}

		r := readMessage{Row: row}
		if !left.spend(len(row.Content)) {
			r.WithoutBody, r.Truncated = true, true
		}
		replies = append(replies, r)
	}

	return replies, nil
}

// handOut records that the body of one of the owner's rows was handed out: the
// row is stamped read, once, and its sender is told — see noteFetched. A
// delegated read records nothing.
//
// why the stamp and the telling are one act: handing a body out tells the
// sender it was collected, and a row that says otherwise leaves the two halves
// of one fact disagreeing — the sender reading it collected while unread_only
// still lists it.
//
// why a delegated read records nothing: its reader is not the recipient. A
// stamp would tell the mailbox's identity it read what it did not, and tell the
// sender of a collection its recipient never made.
func (mod *Module) handOut(owner *astral.Identity, row *messaging.StoredMessage, delegated bool) error {
	if delegated {
		return nil
	}

	if err := mod.db.MarkRead(owner, row); err != nil {
		return err
	}
	mod.noteFetched(row)

	return nil
}

// budget is what is left of one answer, in bytes of message body.
type budget int

// spend charges a body against the answer and reports whether it fits. Once the
// budget is spent it stays spent, so every later body is left out too.
func (b *budget) spend(n int) bool {
	*b -= budget(n)
	return *b >= 0
}
