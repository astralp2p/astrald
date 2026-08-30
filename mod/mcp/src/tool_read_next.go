package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	mcpapi "github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

type readNextIn struct {
	From      string `json:"from,omitempty" jsonschema:"claim only from this sender, by identity or alias"`
	Thread    string `json:"thread,omitempty" jsonschema:"claim only from this exchange, by its thread id"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"how long to wait for a message in milliseconds"`
}

func (mod *Module) readNextTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readNextIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readNextIn) (res *mcpsdk.CallToolResult, out messageOut, err error) {
		timeout := mod.config.ReadTimeout
		if in.TimeoutMs > 0 {
			if t := time.Duration(in.TimeoutMs) * time.Millisecond; t < timeout {
				timeout = t
			}
		}

		var q inboxQuery

		// why the sender resolves the way send_message's recipient does: the
		// two ends of one exchange are named the same way or an agent has to
		// hold two forms of the same address.
		if in.From != "" {
			if q.From, err = mod.Dir.ResolveIdentity(in.From); err != nil {
				return nil, out, fmt.Errorf("unknown sender: %v", in.From)
			}
		}
		if in.Thread != "" {
			if q.Thread, err = mcpapi.ParseMessageID(in.Thread); err != nil {
				return nil, out, err
			}
		}

		row, err := mod.claimNext(ctx, agentID, q, timeout)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out.Status = "timeout"
			return nil, out, nil
		}
		if err != nil {
			return nil, out, err
		}

		mod.noteFetched(row)

		out = messageResult(mod, row)
		out.Status = "message"
		return nil, out, nil
	}
}
