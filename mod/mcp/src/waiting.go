package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// waitRequest is a park on the agent's inbox. A zero Timeout takes the
// deployment's default window. A nil Report is a caller that named no progress
// token.
type waitRequest struct {
	From    string
	Since   string
	Timeout time.Duration
	Report  progressFunc
}

// waitAnswer is what one park came back with: the rows, the window it was
// given, and the time it actually held.
type waitAnswer struct {
	Rows    []*mcp.StoredMessage
	Granted time.Duration
	Waited  time.Duration
}

// waitMessages parks until the agent's inbox holds a message it has not put
// away, and answers what it found. It stamps nothing: the park and the read are
// separate acts.
//
// why the grant is min(ask, ceiling) and never a refusal: refusing would make
// the deployment's ceiling part of every client's configuration, where a clamp
// is read from the answer, which names what was granted.
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
	ans.Rows, err = mod.pollMessages(ctx, agentID, pollRequest{
		Query:   q,
		Timeout: ans.Granted,
		Report:  req.Report,
	})
	ans.Waited = time.Since(start)

	return ans, err
}
