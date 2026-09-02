package mcp

import (
	"context"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendMessageIn struct {
	To       string `json:"to" jsonschema:"recipient agent identity or alias"`
	Content  string `json:"content" jsonschema:"the message body"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"the id of the message this answers; omit when it answers none"`
}

type sendMessageOut struct {
	ID string `json:"id" jsonschema:"the id this message is stored under, in your outbox and in the recipient's inbox alike; pass it as parent_id to relate a later message to this one"`
}

func (mod *Module) sendMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[sendMessageIn, sendMessageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sendMessageIn) (res *mcpsdk.CallToolResult, out sendMessageOut, err error) {
		var parent mcpapi.MessageID
		if in.ParentID != "" {
			if parent, err = mcpapi.ParseMessageID(in.ParentID); err != nil {
				return nil, out, err
			}
		}

		id, err := mod.sendMessage(agentID, in.To, in.Content, parent)
		if err != nil {
			return nil, out, err
		}

		out.ID = id.String()
		return nil, out, nil
	}
}
