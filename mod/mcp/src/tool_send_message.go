package mcp

import (
	"context"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendMessageIn struct {
	To      string `json:"to" jsonschema:"recipient agent identity or alias"`
	Content string `json:"content" jsonschema:"the message body"`
}

type sendMessageOut struct {
	ID string `json:"id" jsonschema:"the id the message is stored under"`
}

func (mod *Module) sendMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[sendMessageIn, sendMessageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sendMessageIn) (res *mcpsdk.CallToolResult, out sendMessageOut, err error) {
		id, err := mod.sendMessage(agentID, in.To, in.Content)
		if err != nil {
			return nil, out, err
		}

		out.ID = id.String()
		return nil, out, nil
	}
}
