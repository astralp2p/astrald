package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// The five things an agent does with its mail. Each mail tool is one call into
// this file: the tool names the arguments and renders the answer, and every
// decision about what the mailbox does is made on this side of the line.
//
// why the requests carry the agent's words and not the store's: resolving a
// correspondent, defaulting a list, bounding an answer and refusing a filter are
// all the module's, so a tool that did any of them would be a second place the
// rules live.

// waitRequest is a park on the agent's inbox. A zero Timeout takes the
// deployment's default window.
type waitRequest struct {
	From    string
	Since   string
	Timeout time.Duration
}

// waitAnswer is what one park came back with: the rows, the window it was
// given, and the time it actually held.
type waitAnswer struct {
	Rows    []dbMessage
	Granted time.Duration
	Waited  time.Duration
}

// waitMessages parks until the agent's inbox holds a message it has not put
// away, and answers what it found. It stamps nothing: the park and the read are
// separate acts, so two agents waiting at once are answered the same messages
// and an agent that stops between the answer and the work leaves the mailbox as
// it was.
//
// why the grant is min(ask, ceiling) and never a refusal: the ceiling is the
// deployment's, set against the chain that carries the call, and a refusal
// would make that ceiling part of every client's configuration. A clamp needs
// no coordination to be read, because the answer names what was granted.
func (mod *Module) waitMessages(ctx context.Context, agentID *astral.Identity, req waitRequest) (waitAnswer, error) {
	var ans waitAnswer

	q, err := mod.query(listRequest{List: listInbox, From: req.From, Since: req.Since})
	if err != nil {
		return ans, err
	}

	ans.Granted = mod.config.WaitDefault
	if req.Timeout > 0 {
		ans.Granted = req.Timeout
	}
	ans.Granted = min(ans.Granted, mod.config.WaitMax)

	start := time.Now()
	ans.Rows, err = mod.pollMessages(ctx, agentID, q, ans.Granted)
	ans.Waited = time.Since(start)

	return ans, err
}

// ── reading ────────────────────────────────────────────────────────────────
