package mcp

import (
	"context"
	"errors"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
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
	ID          string `json:"id,omitempty" jsonschema:"the id the message is stored under"`
	Sender      string `json:"sender,omitempty" jsonschema:"sender identity, and the address a reply goes to"`
	SenderAlias string `json:"sender_alias,omitempty" jsonschema:"sender display name"`
	Content     string `json:"content,omitempty" jsonschema:"the message body"`
	Thread      string `json:"thread,omitempty" jsonschema:"the exchange this belongs to; name it when you reply"`
	StoredAt    string `json:"stored_at,omitempty" jsonschema:"when this node stored the message"`
}

func (mod *Module) readMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readMessageIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readMessageIn) (res *mcpsdk.CallToolResult, out messageOut, err error) {
		id, err := mcpapi.ParseMessageID(in.ID)
		if err != nil {
			return nil, out, err
		}

		row, err := mod.db.ReadMessage(agentID, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, out, errNoSuchMessage
		}
		if err != nil {
			return nil, out, err
		}

		mod.noteFetched(row)

		return nil, messageResult(mod, row), nil
	}
}

// messageResult renders a stored message for a tool result.
func messageResult(mod *Module, row *dbMessage) messageOut {
	return messageOut{
		ID:          row.ID.String(),
		Sender:      row.Sender.String(),
		SenderAlias: mod.Dir.DisplayName(row.Sender),
		Content:     row.Content,
		StoredAt:    stampMessageTime(row.StoredAt),
	}
}

// stampMessageTime renders a stored timestamp for a tool result.
func stampMessageTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// stampOptionalTime renders an instant that may not have happened. The empty
// string is the absence of the fact, and the field is omitted.
func stampOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return stampMessageTime(*t)
}
