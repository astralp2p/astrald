package mcp

import (
	"context"
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
)

type readNextIn struct {
	TimeoutMs int `json:"timeout_ms,omitempty" jsonschema:"how long to wait for a message in milliseconds"`
}

func (mod *Module) readNextTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[readNextIn, messageOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in readNextIn) (res *mcpsdk.CallToolResult, out messageOut, err error) {
		timeout := mod.config.ReadTimeout
		if in.TimeoutMs > 0 {
			if t := time.Duration(in.TimeoutMs) * time.Millisecond; t < timeout {
				timeout = t
			}
		}

		row, err := mod.claimNext(ctx, agentID, timeout)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			out.Status = "timeout"
			return nil, out, nil
		}
		if err != nil {
			return nil, out, err
		}

		out = messageResult(mod, row)
		out.Status = "message"
		return nil, out, nil
	}
}
