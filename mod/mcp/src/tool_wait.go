package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type waitIn struct {
	From      string `json:"from,omitempty" jsonschema:"wait only for this correspondent, by identity or alias"`
	Since     string `json:"since,omitempty" jsonschema:"wait only for what arrives after this, as a previous answer's next_since"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"how long to park, in milliseconds; the deployment's ceiling is the most you get"`
}

type waitOut struct {
	Messages  []messageEntry `json:"messages" jsonschema:"what arrived, without their bodies; read them with read_messages"`
	NextSince string         `json:"next_since,omitempty" jsonschema:"pass back as since on the next wait"`
	TimedOut  bool           `json:"timed_out" jsonschema:"the window closed with nothing new"`
}

// waitTool parks until the agent's inbox holds a message it has not put away.
//
// why it answers envelopes and not ids alone: an agent waiting on one answer
// that is woken by a stranger would otherwise have to read the stranger's body
// to find out it is one — which stamps it read and tells its sender the body was
// collected. The sender is on every envelope, so the agent decides without
// opening anything.
func (mod *Module) waitTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[waitIn, waitOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in waitIn) (res *mcpsdk.CallToolResult, out waitOut, err error) {
		rows, err := mod.waitMessages(ctx, agentID, waitRequest{
			From:    in.From,
			Since:   in.Since,
			Timeout: time.Duration(in.TimeoutMs) * time.Millisecond,
		})
		if err != nil {
			return nil, out, err
		}

		out.Messages = mod.entries(rows)
		out.NextSince = nextSince(rows)
		out.TimedOut = len(rows) == 0

		return nil, out, nil
	}
}
