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
//
// why it stamps nothing: the park and the read are separate acts. Two agents
// waiting at once are answered the same messages, neither takes anything from
// the other, and an agent that stops between the answer and the work leaves the
// mailbox as it was.
func (mod *Module) waitTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[waitIn, waitOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in waitIn) (res *mcpsdk.CallToolResult, out waitOut, err error) {
		// why the ceiling only shortens: it is the deployment's, set against the
		// clients it serves, and a park that outlives its client answers a
		// connection nobody is reading.
		timeout := mod.config.WaitTimeout
		if in.TimeoutMs > 0 {
			if t := time.Duration(in.TimeoutMs) * time.Millisecond; t < timeout {
				timeout = t
			}
		}

		q := messageQuery{List: listInbox}
		if in.Since != "" {
			if q.Since, err = parseSince(in.Since); err != nil {
				return nil, out, err
			}
		}
		if in.From != "" {
			if q.From, err = mod.Dir.ResolveIdentity(in.From); err != nil {
				return nil, out, errUnknownPeer(in.From)
			}
		}
		if err = q.validate(); err != nil {
			return nil, out, err
		}

		rows, err := mod.waitForMessages(ctx, agentID, q, timeout)
		if err != nil {
			return nil, out, err
		}

		if len(rows) == 0 {
			out.TimedOut = true
			out.Messages = []messageEntry{}
			return nil, out, nil
		}

		out.Messages = make([]messageEntry, len(rows))
		for i, row := range rows {
			out.Messages[i] = mod.entry(row)
		}
		out.NextSince = nextSince(rows)

		return nil, out, nil
	}
}
