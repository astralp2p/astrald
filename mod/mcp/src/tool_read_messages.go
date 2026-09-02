package mcp

import (
	"context"

	"github.com/astralp2p/astral-go/api/mcp"
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
		refs := make([]messageRef, len(in.IDs))
		for i, r := range in.IDs {
			if refs[i], err = parseRef(r.Box, r.ID); err != nil {
				return nil, out, err
			}
		}

		result, err := mod.readMessages(agentID, readRequest{
			Refs:        refs,
			Children:    in.Children,
			MaxChildren: in.MaxChildren,
		})
		if err != nil {
			return nil, out, err
		}

		out.Messages = wholes(result.Messages)
		out.Replies = wholes(result.Replies)
		for _, ref := range result.NotFound {
			out.NotFound = append(out.NotFound, messageRefIn{Box: ref.Box, ID: ref.ID.String()})
		}

		return nil, out, nil
	}
}

// wholes renders what a read decided about each message it answers.
func wholes(list []readMessage) []messageOut {
	if len(list) == 0 {
		return nil
	}

	out := make([]messageOut, len(list))
	for i, m := range list {
		out[i] = whole(m.Row)
		if len(m.ChildIDs) > 0 {
			out[i].ChildIDs = make([]string, len(m.ChildIDs))
			for j, id := range m.ChildIDs {
				out[i].ChildIDs[j] = id.String()
			}
		}
		if m.WithoutBody {
			out[i].Content = ""
		}
		out[i].Truncated = m.Truncated
	}
	return out
}

// whole renders one message with its body.
func whole(m *mcp.StoredMessage) messageOut {
	out := messageOut{
		ID:        m.ID.String(),
		Box:       string(m.Box),
		Sender:    m.Sender.String(),
		Recipient: m.Recipient.String(),
		Content:   string(m.Content),
		CreatedAt: stampMessageTime(m.CreatedAt),
	}
	if !m.ParentID.IsZero() {
		out.ParentID = m.ParentID.String()
	}
	return out
}
