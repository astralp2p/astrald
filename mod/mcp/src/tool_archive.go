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

type archiveOut struct {
	Archived bool `json:"archived" jsonschema:"this call is the one that changed it; false means it was already so"`
}

// archiveTool puts one message away, or puts it back.
//
// why RowsAffected is the answer: admission and write are one statement, so the
// count says both whether the message was the agent's and whether this call is
// the one that changed it. A lookup then a write would race.
func (mod *Module) archiveTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[archiveIn, archiveOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in archiveIn) (res *mcpsdk.CallToolResult, out archiveOut, err error) {
		ref, err := parseRef(messageRefIn{Box: in.Box, ID: in.ID})
		if err != nil {
			return nil, out, err
		}

		var n int64
		if in.Undo {
			n, err = mod.db.Unarchive(agentID, ref.Box, ref.ID)
		} else {
			n, err = mod.db.Archive(agentID, ref.Box, ref.ID)
		}
		if err != nil {
			return nil, out, err
		}

		out.Archived = n == 1
		return nil, out, nil
	}
}

// errUnknownPeer is the one answer a name that does not resolve gets, wherever
// it is named. A caller cannot tell a correspondent it may not reach from one
// that does not exist.
func errUnknownPeer(name string) error {
	return &unknownPeerError{name}
}

type unknownPeerError struct{ name string }

func (e *unknownPeerError) Error() string { return "unknown correspondent: " + e.name }

var _ = astral.Identity{}
