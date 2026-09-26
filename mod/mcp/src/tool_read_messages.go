package mcp

import (
	"context"
	"fmt"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
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
// why replies are answered flat beside these: a nested type refers to itself,
// which the SDK's schema generator refuses outright. Each reply names the
// message it answers, which carries the same edge.
type messageOut struct {
	ID        string `json:"id"`
	Box       string `json:"box" jsonschema:"inbox or outbox"`
	Sender    string `json:"sender" jsonschema:"who wrote it"`
	Recipient string `json:"recipient" jsonschema:"who it was written to"`
	Content   string `json:"content" jsonschema:"the message body"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"the message this answers"`
	CreatedAt string `json:"created_at"`

	ChildIDs  []string `json:"child_ids,omitempty" jsonschema:"the ids of every direct reply to this message, oldest first; read any of them with read_messages and this message's box"`
	Truncated bool     `json:"truncated,omitempty" jsonschema:"the body was left out because this answer was already full; read this message on its own"`
}

type readMessagesOut struct {
	Messages []messageOut   `json:"messages" jsonschema:"the messages you named"`
	Replies  []messageOut   `json:"replies,omitempty" jsonschema:"their direct replies, oldest first; each names the message it answers in parent_id"`
	NotFound []messageRefIn `json:"not_found,omitempty" jsonschema:"ids you do not hold; the rest were still read"`
}

func (mod *Module) readMessagesTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readMessagesIn, readMessagesOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readMessagesIn) (res *mcpsdk.CallToolResult, out readMessagesOut, err error) {
		req, err := readRequestOf(in)
		if err != nil {
			return nil, out, err
		}

		result, err := mod.Messaging.ReadMessages(ctx, agentID, req)
		if err != nil {
			return nil, out, err
		}

		out.Messages = wholes(result.Messages)
		out.Replies = wholes(result.Replies)
		for _, ref := range result.NotFound {
			out.NotFound = append(out.NotFound, messageRefIn{Box: string(ref.Box), ID: ref.ID.String()})
		}

		return nil, out, nil
	}
}

// readRequestOf turns the agent's words into a read request, refusing a ref it
// cannot parse before anything is read.
//
// why max_children is bounded here as well as in messaging: the request field is
// sixteen bits, and an ask past it would wrap to a smaller one instead of being
// clamped to the module's bound.
func readRequestOf(in readMessagesIn) (*messaging.ReadMessagesRequest, error) {
	if in.MaxChildren < 0 {
		in.MaxChildren = 0
	}

	req := &messaging.ReadMessagesRequest{
		Children:    astral.String8(in.Children),
		MaxChildren: astral.Uint16(min(in.MaxChildren, messaging.MaxChildren)),
	}
	for _, r := range in.IDs {
		ref, err := parseRef(r.Box, r.ID)
		if err != nil {
			return nil, err
		}
		req.Refs = append(req.Refs, &ref)
	}
	return req, nil
}

// parseRef reads the pair a listing handed out. The box is not optional and is
// never inferred: an id alone names a row in each direction, and the archive
// spans both.
func parseRef(box, id string) (ref messaging.MessageRef, err error) {
	if box != messaging.BoxInbox && box != messaging.BoxOutbox {
		return ref, fmt.Errorf("box is inbox or outbox, not %v", box)
	}
	if ref.ID, err = messaging.ParseMessageID(id); err != nil {
		return ref, err
	}
	ref.Box = astral.String8(box)
	return ref, nil
}

// wholes renders what a read decided about each message it answers.
func wholes(list []*messaging.ReadMessage) []messageOut {
	if len(list) == 0 {
		return nil
	}

	out := make([]messageOut, len(list))
	for i, m := range list {
		out[i] = whole(m)
		if len(m.ChildIDs) > 0 {
			out[i].ChildIDs = make([]string, len(m.ChildIDs))
			for j, id := range m.ChildIDs {
				out[i].ChildIDs[j] = id.String()
			}
		}
		out[i].Truncated = bool(m.Truncated)
	}
	return out
}

// whole renders one message, with its body where the read handed it out.
func whole(m *messaging.ReadMessage) messageOut {
	e := m.Envelope
	out := messageOut{
		ID:        e.ID.String(),
		Box:       string(e.Box),
		Sender:    e.Sender.String(),
		Recipient: e.Recipient.String(),
		CreatedAt: stampMessageTime(e.CreatedAt),
	}
	if m.Content != nil {
		out.Content = string(*m.Content)
	}
	if !e.ParentID.IsZero() {
		out.ParentID = e.ParentID.String()
	}
	return out
}
