package mcp

import (
	"context"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultInboxLimit = 50
	maxInboxLimit     = 200
)

type inboxIn struct {
	UnreadOnly bool `json:"unread_only,omitempty" jsonschema:"list only the messages not read yet"`
	Limit      int  `json:"limit,omitempty" jsonschema:"how many messages to list, oldest first"`
}

// inboxEntry is one message without its body: what the inbox lists is who
// wrote, when, and whether it has been read.
type inboxEntry struct {
	ID          string `json:"id" jsonschema:"pass to read_message to read the body"`
	Sender      string `json:"sender" jsonschema:"sender identity"`
	SenderAlias string `json:"sender_alias,omitempty" jsonschema:"sender display name"`
	DeliveredAt string `json:"delivered_at" jsonschema:"when the message arrived"`
	Read        bool   `json:"read" jsonschema:"the message has been read"`
}

type inboxOut struct {
	Messages []inboxEntry `json:"messages" jsonschema:"waiting messages, oldest first"`
}

func (mod *Module) inboxTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[inboxIn, inboxOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in inboxIn) (res *mcpsdk.CallToolResult, out inboxOut, err error) {
		limit := defaultInboxLimit
		if in.Limit > 0 {
			limit = min(in.Limit, maxInboxLimit)
		}

		rows, err := mod.db.ListInbox(agentID, in.UnreadOnly, limit)
		if err != nil {
			return nil, out, err
		}

		out.Messages = make([]inboxEntry, len(rows))
		for i, row := range rows {
			out.Messages[i] = inboxEntry{
				ID:          row.ID.String(),
				Sender:      row.Sender.String(),
				SenderAlias: mod.Dir.DisplayName(row.Sender),
				DeliveredAt: stampMessageTime(row.DeliveredAt),
				Read:        row.ReadAt != nil,
			}
		}

		return nil, out, nil
	}
}
