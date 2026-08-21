package mcp

import (
	"bytes"
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type receiveIn struct {
	SessionID string `json:"session_id" jsonschema:"session handle from astral-listen or astral-query"`
	TimeoutMs int    `json:"timeout_ms,omitempty" jsonschema:"how long to wait for data in milliseconds"`
}

type receiveOut struct {
	Status   string `json:"status" jsonschema:"data, timeout or closed"`
	Objects  []any  `json:"objects,omitempty" jsonschema:"received objects (objects-format session)"`
	Payload  string `json:"payload,omitempty" jsonschema:"received bytes (raw-format session)"`
	Encoding string `json:"encoding,omitempty" jsonschema:"payload encoding: utf8 or base64"`
	Closed   bool   `json:"closed,omitempty" jsonschema:"the remote side has ended the session"`
}

func (mod *Module) receiveTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[receiveIn, receiveOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in receiveIn) (res *mcpsdk.CallToolResult, out receiveOut, err error) {
		s, err := mod.agentSession(agentID, in.SessionID)
		if err != nil {
			return nil, out, err
		}

		timeout := mod.config.ListenTimeout
		if in.TimeoutMs > 0 {
			if t := time.Duration(in.TimeoutMs) * time.Millisecond; t < timeout {
				timeout = t
			}
		}

		msgs, closed, err := s.receive(ctx, timeout, mod.config.MaxResponseBytes, mod.config.MaxResponseObjects)
		if err != nil {
			return nil, out, err
		}

		out.Closed = closed
		if closed {
			mod.closeSession(s.id)
		}

		switch {
		case len(msgs) > 0:
			out.Status = "data"
		case closed:
			out.Status = "closed"
		default:
			out.Status = "timeout"
		}

		if s.format == sessionFormatObjects {
			for _, msg := range msgs {
				out.Objects = append(out.Objects, jsonValue(msg))
			}
		} else {
			out.Payload, out.Encoding = encodePayload(bytes.Join(msgs, nil))
		}

		return nil, out, nil
	}
}
