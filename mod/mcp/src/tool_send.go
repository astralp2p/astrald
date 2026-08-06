package mcp

import (
	"context"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type sendIn struct {
	SessionID string `json:"session_id" jsonschema:"session handle from astral-listen or astral-query"`
	Data      string `json:"data,omitempty" jsonschema:"data to write"`
	Base64    bool   `json:"base64,omitempty" jsonschema:"data is base64-encoded binary"`
	Close     bool   `json:"close,omitempty" jsonschema:"close the session after writing"`
}

type sendOut struct {
	Ok           bool `json:"ok"`
	BytesWritten int  `json:"bytes_written"`
	Closed       bool `json:"closed,omitempty"`
}

func (mod *Module) sendTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[sendIn, sendOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in sendIn) (res *mcpsdk.CallToolResult, out sendOut, err error) {
		s, err := mod.agentSession(agentID, in.SessionID)
		if err != nil {
			return nil, out, err
		}

		data, err := decodePayload(in.Data, in.Base64)
		if err != nil {
			return nil, out, err
		}

		if len(data) > 0 {
			out.BytesWritten, err = s.send(data)
			if err != nil {
				mod.closeSession(s.id)
				out.Closed = true
				return nil, out, err
			}
		}

		if in.Close {
			mod.closeSession(s.id)
			out.Closed = true
		}

		out.Ok = true
		return nil, out, nil
	}
}
