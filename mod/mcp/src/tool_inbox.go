package mcp

import (
	"context"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type inboxIn struct {
	Thread     string `json:"thread,omitempty" jsonschema:"list only one exchange, by its thread id"`
	UnreadOnly bool   `json:"unread_only,omitempty" jsonschema:"list only the messages not read yet"`
	Limit      int    `json:"limit,omitempty" jsonschema:"how many messages to list, oldest first"`
}

// inboxEntry is one message without its body: what the inbox lists is who
// wrote, when, and whether it has been read.
type inboxEntry struct {
	ID          string `json:"id" jsonschema:"pass to read_message to read the body"`
	Sender      string `json:"sender" jsonschema:"sender identity"`
	SenderAlias string `json:"sender_alias,omitempty" jsonschema:"sender display name"`
	Thread      string `json:"thread" jsonschema:"the exchange this belongs to; pass to read_next or inbox to follow it"`
	StoredAt    string `json:"stored_at" jsonschema:"when this node stored the message"`
	Read        bool   `json:"read" jsonschema:"the message has been read"`
}

type inboxOut struct {
	Messages []inboxEntry `json:"messages" jsonschema:"waiting messages, oldest first"`
}

func (mod *Module) inboxTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[inboxIn, inboxOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in inboxIn) (res *mcpsdk.CallToolResult, out inboxOut, err error) {
		q := inboxQuery{UnreadOnly: in.UnreadOnly, Limit: in.Limit}
		if in.Thread != "" {
			if q.Thread, err = mcpapi.ParseMessageID(in.Thread); err != nil {
				return nil, out, err
			}
		}

		rows, err := mod.listInbox(agentID, q)
		if err != nil {
			return nil, out, err
		}

		out.Messages = make([]inboxEntry, len(rows))
		for i, row := range rows {
			out.Messages[i] = inboxEntry{
				ID:          row.ID.String(),
				Sender:      row.Sender.String(),
				SenderAlias: mod.Dir.DisplayName(row.Sender),
				Thread:      row.Thread.String(),
				StoredAt:    stampMessageTime(row.StoredAt),
				Read:        row.ReadAt != nil,
			}
		}

		return nil, out, nil
	}
}
