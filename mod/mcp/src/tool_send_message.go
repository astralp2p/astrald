package mcp

import (
	"context"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendMessageIn struct {
	To      string `json:"to" jsonschema:"recipient agent identity or alias"`
	Content string `json:"content" jsonschema:"the message body"`
	Thread  string `json:"thread,omitempty" jsonschema:"the exchange to send into; omit to start one"`
}

type sendMessageOut struct {
	ID     string `json:"id" jsonschema:"the id the message is stored under"`
	Thread string `json:"thread" jsonschema:"the exchange it went into; name it to follow the answer"`
}

func (mod *Module) sendMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[sendMessageIn, sendMessageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sendMessageIn) (res *mcpsdk.CallToolResult, out sendMessageOut, err error) {
		var thread mcpapi.MessageID
		if in.Thread != "" {
			if thread, err = mcpapi.ParseMessageID(in.Thread); err != nil {
				return nil, out, err
			}
		}

		id, sent, err := mod.sendMessage(agentID, in.To, in.Content, thread)
		if err != nil {
			return nil, out, err
		}

		out.ID, out.Thread = id.String(), sent.String()
		return nil, out, nil
	}
}
