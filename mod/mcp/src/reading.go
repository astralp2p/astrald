package mcp

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// The five things an agent does with its mail. Each mail tool is one call into
// this file: the tool names the arguments and renders the answer, and every
// decision about what the mailbox does is made on this side of the line.
//
// why the requests carry the agent's words and not the store's: resolving a
// correspondent, defaulting a list, bounding an answer and refusing a filter are
// all the module's, so a tool that did any of them would be a second place the
// rules live.

// The module's own bounds on one answer. A body may be 64 KiB and a message may
// have any number of replies, so an unbounded read is the caller deciding how
// much of the reader's context a stranger fills.
const (
	maxReadIDs      = 20
	maxChildren     = 10
	defaultChildren = 10
)

// How much of each message's replies a read answers.
const (
	childrenNone      = "none"
	childrenEnvelopes = "envelopes"
	childrenFull      = "full"
)

type readRequest struct {
	Refs        []messageRef
	Children    string
	MaxChildren int
}

// validate refuses a read the module will not serve and fills in what the
// caller left out.
func (req *readRequest) validate() error {
	// why a repeat is dropped rather than refused: naming one message twice
	// asks for it once, and a read that charged it twice would spend a budget
	// the caller cannot see on a message it already has. The bound is on
	// distinct messages, so the count is taken after the drop.
	req.Refs = distinctRefs(req.Refs)

	switch {
	case len(req.Refs) == 0:
		return errors.New("name at least one message to read")
	case len(req.Refs) > maxReadIDs:
		return fmt.Errorf("name at most %v messages in one read", maxReadIDs)
	}

	if req.Children == "" {
		req.Children = childrenEnvelopes
	}
	switch req.Children {
	case childrenNone, childrenEnvelopes, childrenFull:
	default:
		return fmt.Errorf("children is none, envelopes or full, not %v", req.Children)
	}

	if req.MaxChildren <= 0 {
		req.MaxChildren = defaultChildren
	}
	req.MaxChildren = min(req.MaxChildren, maxChildren)

	return nil
}

// distinctRefs keeps the first of each named row, in the order the caller named
// them. The box is part of the identity: an agent that holds both rows of one id
// named two messages, not one twice.
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
	Row dbMessage

	// ChildIDs are the ids of its direct replies, oldest first — the whole
	// set, whatever the answer carried of them.
	ChildIDs []mcp.MessageID

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

// readMessages reads whole messages the agent holds, with their direct replies.
//
// why the replies are a flat set beside the messages: a reply names the one
// message it answers, so the edge is on the reply and a nested answer would
// carry the same information in a shape no schema can describe.
func (mod *Module) readMessages(agentID *astral.Identity, req readRequest) (res readResult, err error) {
	if err = req.validate(); err != nil {
		return res, err
	}

	rows, missing, err := mod.db.ReadMany(agentID, req.Refs)
	if err != nil {
		return res, err
	}

	left := budget(mod.config.MaxResponseBytes)

	for _, row := range rows {
		mod.noteFetched(&row)

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
		if m.ChildIDs, err = mod.db.ChildIDs(agentID, row.ID); err != nil {
			return res, err
		}

		if req.Children != childrenNone {
			var replies []readMessage
			if replies, err = mod.readReplies(agentID, row.ID, req, &left); err != nil {
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
// for. One level: walking further is the reader's, which the child ids on every
// message it answers are what make possible.
//
// why a child's body is opt-in: handing one out stamps it read and tells its
// sender the body was collected, so a reader that asked about a message would
// otherwise report having collected mail it never asked for.
func (mod *Module) readReplies(owner *astral.Identity, parent mcp.MessageID, req readRequest, left *budget) (replies []readMessage, err error) {
	rows, err := mod.db.Children(owner, parent, req.MaxChildren)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if req.Children == childrenEnvelopes {
			replies = append(replies, readMessage{Row: row, WithoutBody: true})
			continue
		}

		// why the stamp and the handout are one act: handing a body out tells
		// the sender it was collected, and a row that says otherwise leaves the
		// two halves of one fact disagreeing — the sender reading it collected
		// while unread_only still lists it.
		if err = mod.db.MarkRead(owner, &row); err != nil {
			return nil, err
		}
		mod.noteFetched(&row)

		r := readMessage{Row: row}
		if !left.spend(len(row.Content)) {
			r.WithoutBody, r.Truncated = true, true
		}
		replies = append(replies, r)
	}

	return replies, nil
}

// budget is what is left of one answer, in bytes of message body.
type budget int

// spend charges a body against the answer and reports whether it fits. Once the
// budget is spent it stays spent, so every later body is left out too.
func (b *budget) spend(n int) bool {
	*b -= budget(n)
	return *b >= 0
}

// ── archiving ──────────────────────────────────────────────────────────────
