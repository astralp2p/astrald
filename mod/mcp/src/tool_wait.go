package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
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
// why it answers envelopes and not ids alone: an agent woken by a stranger
// would otherwise read the stranger's body to find out it is one, stamping it
// read. The sender is on every envelope.
//
// why the answer names granted_secs beside waited_secs: the grant is the
// deployment's and the ask the caller's, so a clamp reads as two numbers rather
// than as silence the caller misreads.
//
// why a held park reports progress: a client bounds a call it hears nothing on,
// and the bound is the client's rather than the node's. A notification per
// floor interval is the protocol's way to say the park is alive, and it is what
// lets a caller spend the window it was granted.
func (mod *Module) waitTool(agentID *astral.Identity) mcpsdk.ToolHandlerFor[waitIn, waitOut] {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest, in waitIn) (res *mcpsdk.CallToolResult, out waitOut, err error) {
		since, err := parseSince(in.Since)
		if err != nil {
			return nil, out, err
		}

		ans, err := mod.Messaging.Wait(ctx, agentID, messaging.WaitRequest{
			From:    in.From,
			Since:   since,
			Timeout: time.Duration(in.TimeoutSecs) * time.Second,
		}, waitProgress(ctx, req))
		if err != nil {
			return nil, out, err
		}

		out.Messages = entries(ans.Messages)
		out.NextSince = answerSince(uint64(ans.NextSince), in.Since)
		out.TimedOut = bool(ans.TimedOut)
		out.GrantedSecs = wholeSeconds(time.Duration(ans.Granted))
		out.WaitedSecs = wholeSeconds(time.Duration(ans.Waited))

		return nil, out, nil
	}
}

// waitProgress answers the reporter for one call, or nil where the caller named
// no progress token. A notification may name only a token from an active
// request, so a caller that sent none is told nothing.
//
// why the report is never gated on the caller acting on it: a client attaches a
// token from its own SDK, and some attach one and discard every notification.
// The node cannot tell those apart, so it reports to whoever asked.
func waitProgress(ctx context.Context, req *mcpsdk.CallToolRequest) messagingmod.ProgressFunc {
	if req == nil || req.Params == nil || req.Session == nil {
		return nil
	}

	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}

	session := req.Session

	return func(spent, granted time.Duration) {
		// why the error is dropped: the park is what the caller waits for, and
		// a notification that failed to send is not worth ending it.
		_ = session.NotifyProgress(ctx, &mcpsdk.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      spent.Seconds(),
			Total:         granted.Seconds(),
			Message:       "waiting for mail",
		})
	}
}

// wholeSeconds renders a duration for a tool answer, to the nearest second.
func wholeSeconds(d time.Duration) int {
	return int((d + time.Second/2) / time.Second)
}
