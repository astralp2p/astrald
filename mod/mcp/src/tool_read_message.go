package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

var errNoSuchMessage = errors.New("no such message")

type readMessageIn struct {
	ID string `json:"id" jsonschema:"message id, from inbox"`
}

// messageOut is one whole message. Status is set by read_next, which answers
// when no message arrived; read_message answers a message or an error.
type messageOut struct {
	Status      string `json:"status,omitempty" jsonschema:"message or timeout"`
	ID          string `json:"id,omitempty" jsonschema:"message id; a reply carries it in reply_to"`
	Sender      string `json:"sender,omitempty" jsonschema:"sender identity, and the address a reply goes to"`
	SenderAlias string `json:"sender_alias,omitempty" jsonschema:"sender display name"`
	Topic       string `json:"topic,omitempty" jsonschema:"what the message is about"`
	Content     string `json:"content,omitempty" jsonschema:"the message body"`
	ReplyTo     string `json:"reply_to,omitempty" jsonschema:"id of the message this one answers"`
	DeliveredAt string `json:"delivered_at,omitempty" jsonschema:"when the message arrived"`
}

func (mod *Module) readMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readMessageIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readMessageIn) (res *mcpsdk.CallToolResult, out messageOut, err error) {
		row, err := mod.db.ReadMessage(agentID, in.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, out, errNoSuchMessage
		}
		if err != nil {
			return nil, out, err
		}

		return nil, messageResult(mod, row), nil
	}
}

// messageResult renders a stored message for a tool result.
func messageResult(mod *Module, row *dbMessage) messageOut {
	return messageOut{
		ID:          row.ID,
		Sender:      row.Sender.String(),
		SenderAlias: mod.Dir.DisplayName(row.Sender),
		Topic:       row.Topic,
		Content:     row.Content,
		ReplyTo:     row.ReplyTo,
		DeliveredAt: stampMessageTime(row.DeliveredAt),
	}
}

// stampMessageTime renders a stored timestamp for a tool result.
func stampMessageTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
