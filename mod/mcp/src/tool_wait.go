package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type waitIn struct {
	From        string `json:"from,omitempty" jsonschema:"wait only for this correspondent, by identity or alias"`
	Since       string `json:"since,omitempty" jsonschema:"wait only for what arrives after this, as a previous answer's next_since"`
	TimeoutSecs int    `json:"timeout_secs,omitempty" jsonschema:"how long to park, in seconds; an ask over the deployment's ceiling is granted the ceiling, and granted_secs answers what was given"`
}

type waitOut struct {
	Messages    []messageEntry `json:"messages" jsonschema:"what arrived, without their bodies; read them with read_messages"`
	NextSince   string         `json:"next_since,omitempty" jsonschema:"pass back as since on the next wait; when nothing newer arrived it repeats the since you sent"`
	TimedOut    bool           `json:"timed_out" jsonschema:"the granted window closed with nothing new"`
	GrantedSecs int            `json:"granted_secs" jsonschema:"the window this park was given: your ask or the deployment's default, never over its ceiling"`
	WaitedSecs  int            `json:"waited_secs" jsonschema:"how long this node held the park before answering; near zero means the answer was already waiting"`
}

// waitTool parks until the agent's inbox holds a message it has not put away.
//
// why it answers envelopes and not ids alone: an agent waiting on one answer
// that is woken by a stranger would otherwise have to read the stranger's body
// to find out it is one — which stamps it read and tells its sender the body was
// collected. The sender is on every envelope, so the agent decides without
// opening anything.
//
// why the answer names its window: the grant is the deployment's and the ask
// is the caller's, and where the two differ every conclusion the caller draws
// from the silence is wrong by the difference. granted_secs is the budget and
// waited_secs the spend, so a clamp is two numbers side by side rather than a
// silent lie — and a spend of zero twice in a row says the caller's own
// filters are matching mail it has already seen.
//
// why next_since repeats the caller's cursor when nothing newer arrived: the
// field says pass it back, and an absent value asks the caller to remember
// what it sent. Repeating it makes the instruction followable with no memory.
func (mod *Module) waitTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[waitIn, waitOut] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in waitIn) (res *mcpsdk.CallToolResult, out waitOut, err error) {
		ans, err := mod.waitMessages(ctx, agentID, waitRequest{
			From:    in.From,
			Since:   in.Since,
			Timeout: time.Duration(in.TimeoutSecs) * time.Second,
		})
		if err != nil {
			return nil, out, err
		}

		out.Messages = entries(ans.Rows)
		out.NextSince = nextSince(ans.Rows)
		if out.NextSince == "" {
			out.NextSince = in.Since
		}
		out.TimedOut = len(ans.Rows) == 0
		out.GrantedSecs = wholeSeconds(ans.Granted)
		out.WaitedSecs = wholeSeconds(ans.Waited)

		return nil, out, nil
	}
}

// wholeSeconds renders a duration for a tool answer, to the nearest second.
func wholeSeconds(d time.Duration) int {
	return int((d + time.Second/2) / time.Second)
}
