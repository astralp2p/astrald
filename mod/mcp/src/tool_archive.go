package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/astralp2p/astral-go/astral"
)

type archiveIn struct {
	Box  string `json:"box" jsonschema:"inbox or outbox — which of your rows to put away"`
	ID   string `json:"id" jsonschema:"the message id, as a listing or send_message gave it"`
	Undo bool   `json:"undo,omitempty" jsonschema:"put it back instead"`
}

// why the field is not called "archived": undo runs through the same tool, and
// there "archived: true" would name the opposite of what happened. What the
// call reports is whether it was the one that moved the message.
type archiveOut struct {
	Changed bool `json:"changed" jsonschema:"this call moved the message; false means it was already where you asked for, or you do not hold it"`
}

func (mod *Module) archiveTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[archiveIn, archiveOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in archiveIn) (res *mcpsdk.CallToolResult, out archiveOut, err error) {
		ref, err := parseRef(in.Box, in.ID)
		if err != nil {
			return nil, out, err
		}

		out.Changed, err = mod.archiveMessage(agentID, ref, in.Undo)
		if err != nil {
			return nil, out, err
		}

		return nil, out, nil
	}
}
