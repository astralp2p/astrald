package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type listenIn struct {
	TimeoutMs int `json:"timeout_ms,omitempty" jsonschema:"how long to wait for a query in milliseconds"`
}

type listenOut struct {
	Status      string            `json:"status" jsonschema:"query or timeout"`
	SessionID   string            `json:"session_id,omitempty" jsonschema:"dialog session handle for astral-send/astral-receive"`
	Caller      string            `json:"caller,omitempty" jsonschema:"caller identity"`
	CallerAlias string            `json:"caller_alias,omitempty" jsonschema:"caller display name"`
	Path        string            `json:"path,omitempty" jsonschema:"query path"`
	Params      map[string]string `json:"params,omitempty" jsonschema:"query arguments"`
	Payload     string            `json:"payload,omitempty" jsonschema:"request payload"`
	Encoding    string            `json:"encoding,omitempty" jsonschema:"payload encoding: utf8 or base64"`
}

func (mod *Module) listenTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[listenIn, listenOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in listenIn) (res *mcpsdk.CallToolResult, out listenOut, err error) {
		timeout := mod.config.ListenTimeout
		if in.TimeoutMs > 0 {
			if t := time.Duration(in.TimeoutMs) * time.Millisecond; t < timeout {
				timeout = t
			}
		}

		if s, ok := mod.takePending(agentID); ok {
			return nil, queryResult(mod, s), nil
		}

		ch, err := mod.parkListener(agentID)
		if err != nil {
			return nil, out, err
		}
		defer mod.unparkListener(agentID, ch)

		// why: a query may have queued between the drain above and the park,
		// which would otherwise sit until this call times out.
		if s, ok := mod.takePending(agentID); ok {
			return nil, queryResult(mod, s), nil
		}

		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case s := <-ch:
			return nil, queryResult(mod, s), nil
		case <-timer.C:
			out.Status = "timeout"
			return nil, out, nil
		case <-ctx.Done():
			return nil, out, ctx.Err()
		}
	}
}

func queryResult(mod *Module, s *session) (out listenOut) {
	out.Status = "query"
	out.SessionID = s.id
	out.Caller = s.caller.String()
	out.CallerAlias = mod.Dir.DisplayName(s.caller)
	out.Path = s.path
	out.Params = s.params
	out.Payload, out.Encoding = encodePayload(s.payload)
	return
}
