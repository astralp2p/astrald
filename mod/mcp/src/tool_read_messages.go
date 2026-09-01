package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The module's own bounds on one answer. A body may be 64 KiB, and a message
// may have any number of replies, so an unbounded read is the caller deciding
// how much of the reader's context a stranger fills.
//
// why these are the module's and not the caller's: how much a read hands out in
// one answer is a property of the answer, and a caller naming it is a caller
// naming the reader's cost.
const (
	maxReadIDs      = 20
	maxChildren     = 10
	defaultChildren = 10
)

type messageRefIn struct {
	Box string `json:"box" jsonschema:"inbox or outbox — as a listing gave it"`
	ID  string `json:"id" jsonschema:"the message id"`
}

type readMessagesIn struct {
	IDs         []messageRefIn `json:"ids" jsonschema:"the messages to read, each named by box and id"`
	Children    string         `json:"children,omitempty" jsonschema:"none, envelopes or full — how much of each message's replies to answer; envelopes by default"`
	MaxChildren int            `json:"max_children,omitempty" jsonschema:"how many replies to answer per message"`
}

// messageOut is one whole message.
//
// why replies are answered flat beside these rather than nested inside them: a
// nested type refers to itself, which no schema can describe — the SDK refuses
// it outright — and a depth-first tree has to be linearised before the most
// common question, what is newest, can be answered at all. Each reply carries
// the parent it answers, which is the same information in the shape a reader
// already holds its own conversation in.
type messageOut struct {
	ID        string `json:"id"`
	Box       string `json:"box" jsonschema:"inbox or outbox"`
	Sender    string `json:"sender" jsonschema:"who wrote it"`
	Recipient string `json:"recipient" jsonschema:"who it was written to"`
	PeerAlias string `json:"peer_alias,omitempty" jsonschema:"the other party's display name"`
	Content   string `json:"content" jsonschema:"the message body"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"the message this answers"`
	CreatedAt string `json:"created_at"`

	MoreChildren int  `json:"more_children,omitempty" jsonschema:"how many of its replies this answer left; list them with list_messages and match on parent_id"`
	Truncated    bool `json:"truncated,omitempty" jsonschema:"the body was left out because this answer was already full; read this message on its own"`
}

type readMessagesOut struct {
	Messages []messageOut   `json:"messages" jsonschema:"the messages you named"`
	Replies  []messageOut   `json:"replies,omitempty" jsonschema:"their direct replies, oldest first; each names the message it answers in parent_id"`
	NotFound []messageRefIn `json:"not_found,omitempty" jsonschema:"ids you do not hold; the rest were still read"`
}

func (mod *Module) readMessagesTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readMessagesIn, readMessagesOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readMessagesIn) (res *mcpsdk.CallToolResult, out readMessagesOut, err error) {
		if len(in.IDs) == 0 {
			return nil, out, errors.New("name at least one message to read")
		}
		if len(in.IDs) > maxReadIDs {
			return nil, out, fmt.Errorf("name at most %v messages in one read", maxReadIDs)
		}

		depth := in.Children
		if depth == "" {
			depth = childrenEnvelopes
		}
		if depth != childrenNone && depth != childrenEnvelopes && depth != childrenFull {
			return nil, out, fmt.Errorf("children is none, envelopes or full, not %v", depth)
		}

		kids := in.MaxChildren
		if kids <= 0 {
			kids = defaultChildren
		}
		kids = min(kids, maxChildren)

		refs := make([]messageRef, 0, len(in.IDs))
		for _, r := range in.IDs {
			ref, err := parseRef(r)
			if err != nil {
				return nil, out, err
			}
			refs = append(refs, ref)
		}

		rows, missing, err := mod.db.ReadMany(agentID, refs)
		if err != nil {
			return nil, out, err
		}

		budget := mod.config.MaxResponseBytes

		for _, row := range rows {
			mod.noteFetched(&row)

			m := mod.whole(row)
			budget -= len(m.Content)
			if depth != childrenNone {
				var replies []messageOut
				if replies, m.MoreChildren, err = mod.replies(agentID, row.ID, depth, kids); err != nil {
					return nil, out, err
				}
				for _, r := range replies {
					budget -= len(r.Content)
					if budget < 0 {
						r.Content = ""
					}
					out.Replies = append(out.Replies, r)
				}
			}
			if budget < 0 {
				// why the body is dropped rather than the message: the caller
				// named these ids, so the answer says what it holds and which
				// bodies it left. A read that quietly returned fewer messages
				// would look like a mailbox that lost them.
				m.Content = ""
				m.Truncated = true
			}
			out.Messages = append(out.Messages, m)
		}

		for _, ref := range missing {
			out.NotFound = append(out.NotFound, messageRefIn{Box: ref.Box, ID: ref.ID.String()})
		}

		return nil, out, nil
	}
}

const (
	childrenNone      = "none"
	childrenEnvelopes = "envelopes"
	childrenFull      = "full"
)

// replies answers a message's direct replies and how many it left. One level:
// walking further is the reader's, and a recursive answer is one the caller
// cannot bound.
//
// why a child's body is opt-in: handing one out stamps it read and tells its
// sender the body was collected, so a reader that asked about a message would
// otherwise report having collected mail it never asked for.
func (mod *Module) replies(owner *astral.Identity, parent mcpapi.MessageID, depth string, limit int) ([]messageOut, int, error) {
	rows, err := mod.db.Children(owner, parent, limit)
	if err != nil {
		return nil, 0, err
	}

	total, err := mod.db.CountChildren(owner, parent)
	if err != nil {
		return nil, 0, err
	}

	kids := make([]messageOut, 0, len(rows))
	for _, row := range rows {
		if depth == childrenEnvelopes {
			m := mod.whole(row)
			m.Content = ""
			kids = append(kids, m)
			continue
		}

		// why the stamp and the handout are one act: handing a body out tells
		// the sender it was collected, and a row that says otherwise leaves the
		// two halves of one fact disagreeing — the sender reading it collected
		// while unread_only still lists it.
		if err := mod.db.MarkRead(owner, &row); err != nil {
			return nil, 0, err
		}
		mod.noteFetched(&row)
		kids = append(kids, mod.whole(row))
	}

	return kids, int(total) - len(rows), nil
}

// whole renders one message with its body.
func (mod *Module) whole(row dbMessage) messageOut {
	peer := row.Sender
	if row.Box == boxOutbox {
		peer = row.Recipient
	}

	m := messageOut{
		ID:        row.ID.String(),
		Box:       row.Box,
		Sender:    row.Sender.String(),
		Recipient: row.Recipient.String(),
		PeerAlias: mod.Dir.DisplayName(peer),
		Content:   row.Content,
		CreatedAt: stampMessageTime(row.CreatedAt),
	}
	if !row.ParentID.IsZero() {
		m.ParentID = row.ParentID.String()
	}
	return m
}

func parseRef(r messageRefIn) (ref messageRef, err error) {
	if r.Box != boxInbox && r.Box != boxOutbox {
		return ref, fmt.Errorf("box is inbox or outbox, not %v", r.Box)
	}
	if ref.ID, err = mcpapi.ParseMessageID(r.ID); err != nil {
		return ref, err
	}
	ref.Box = r.Box
	return ref, nil
}
