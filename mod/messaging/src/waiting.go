package messaging

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
	messagingmod "github.com/astralp2p/astrald/mod/messaging"
)

// Wait parks until the owner's inbox holds a message it has not put away, and
// answers it without bodies. A nil report is a caller that asked for no reports.
func (mod *Module) Wait(ctx context.Context, owner *astral.Identity, req messaging.WaitRequest, report messagingmod.ProgressFunc) (*messaging.WaitResult, error) {
	if !mod.hosts(owner) {
		return nil, errNotParticipant
	}

	ans, err := mod.waitMessages(ctx, owner, waitRequest{
		From:    req.From,
		Since:   req.Since,
		Timeout: req.Timeout,
		Report:  report,
	})
	if err != nil {
		return nil, err
	}

	list := envelopes(ans.Rows)

	return &messaging.WaitResult{
		Messages:  list,
		NextSince: astral.Uint64(messaging.NextSince(list, req.Since)),
		TimedOut:  len(list) == 0,
		Granted:   astral.Duration(ans.Granted),
		Waited:    astral.Duration(ans.Waited),
	}, nil
}

// waitRequest is a park on the owner's inbox. A zero Timeout takes the
// deployment's default window. A nil Report is a caller that asked for no
// reports.
type waitRequest struct {
	From    string
	Since   uint64
	Timeout time.Duration
	Report  messagingmod.ProgressFunc
}

// waitAnswer is what one park came back with: the rows, the window it was
// given, and the time it actually held.
type waitAnswer struct {
	Rows    []*messaging.StoredMessage
	Granted time.Duration
	Waited  time.Duration
}

// waitMessages parks until the owner's inbox holds a message it has not put
// away, and answers what it found. It stamps nothing: the park and the read are
// separate acts.
//
// why the grant is min(ask, ceiling) and never a refusal: refusing would make
// the deployment's ceiling part of every client's configuration, where a clamp
// is read from the answer, which names what was granted.
func (mod *Module) waitMessages(ctx context.Context, owner *astral.Identity, req waitRequest) (waitAnswer, error) {
	var ans waitAnswer

	q, err := mod.query(messaging.ListMessagesRequest{List: messaging.ListInbox, From: req.From, Since: req.Since})
	if err != nil {
		return ans, err
	}

	ans.Granted = mod.config.WaitDefault
	if req.Timeout > 0 {
		ans.Granted = req.Timeout
	}
	ans.Granted = min(ans.Granted, mod.config.WaitMax)

	start := time.Now()
	ans.Rows, err = mod.pollMessages(ctx, owner, pollRequest{
		Query:   q,
		Timeout: ans.Granted,
		Report:  req.Report,
	})
	ans.Waited = time.Since(start)

	return ans, err
}
