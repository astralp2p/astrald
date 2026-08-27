package mcp

import (
	"context"
	"fmt"

	authapi "github.com/astralp2p/astral-go/api/auth"
	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendMessageIn struct {
	To      string `json:"to" jsonschema:"recipient agent identity or alias"`
	Topic   string `json:"topic,omitempty" jsonschema:"what the message is about, in a few words"`
	Content string `json:"content" jsonschema:"the message body"`
	ReplyTo string `json:"reply_to,omitempty" jsonschema:"id of the message this one answers"`
}

type sendMessageOut struct {
	ID string `json:"id" jsonschema:"the message id; a reply to it carries the same value in reply_to"`
}

func (mod *Module) sendMessageTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[sendMessageIn, sendMessageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sendMessageIn) (res *mcpsdk.CallToolResult, out sendMessageOut, err error) {
		targetID, err := mod.Dir.ResolveIdentity(in.To)
		if err != nil {
			return nil, out, fmt.Errorf("unknown recipient: %v", in.To)
		}

		// The same question astral-query asks about the same pair: what this
		// agent may reach is its owner's decision, and a message buys it no
		// reach it did not have.
		//
		// why the refusal reads as an unresolvable recipient: an agent learns
		// that it cannot reach this one, and not whether this one exists.
		if !mod.Auth.Authorize(mod.ctx, &mcpapi.CallAgentAction{
			Action: authapi.NewAction(agentID),
			ToID:   targetID,
		}) {
			return nil, out, fmt.Errorf("unknown recipient: %v", in.To)
		}

		if len(in.Content) > mod.config.MaxPayloadBytes {
			return nil, out, fmt.Errorf("content is over %v bytes", mod.config.MaxPayloadBytes)
		}

		msg := &mcpapi.Message{
			ID:      astral.String8(newMessageID()),
			Topic:   astral.String8(in.Topic),
			Content: astral.String32(in.Content),
			ReplyTo: astral.String8(in.ReplyTo),
		}

		if err = mod.deliverMessage(agentID, targetID, msg); err != nil {
			return nil, out, fmt.Errorf("delivery failed: %v", err)
		}

		out.ID = string(msg.ID)
		return nil, out, nil
	}
}
