package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/api/mcp"
	"github.com/astralp2p/astral-go/astral"
)

// waitFloor is how often a parked wait looks again on its own, under the wake.
//
// why a floor under the wake: a wake fires only where a writer calls it, and a
// missed one makes the tool report timed_out over an inbox holding unarchived
// mail. Ten seconds sits under Config.WaitDefault, so an unnamed park catches a
// missed wake before its own deadline.
const waitFloor = 10 * time.Second

// progressFunc announces a park still held, given what it has spent of its
// window and what it was granted. A nil progressFunc is a caller that named no
// progress token, which is answered nothing.
type progressFunc func(spent, granted time.Duration)

// pollRequest is one held look at an owner's mailbox: what to match, how long
// to hold, how often to look on its own, and who to tell that the hold goes on.
type pollRequest struct {
	Query   messageQuery
	Timeout time.Duration
	Floor   time.Duration
	Report  progressFunc
}

// pollMessages parks until the owner's list holds a message the query matches,
// and answers what it found. Nothing is stamped and nothing is taken, so two
// agents waiting at once are answered the same messages.
func (mod *Module) pollMessages(ctx context.Context, owner *astral.Identity, req pollRequest) ([]*mcp.StoredMessage, error) {
	deadline := time.NewTimer(req.Timeout)
	defer deadline.Stop()

	// why the registration precedes the first look: a row landing between the
	// query and the subscribe signals into a registry this waiter is not yet
	// in, so the token is never made. The buffered channel closes the other
	// window; neither alone is enough.
	woke, leave := mod.waiters.park(owner)
	defer leave()

	every := req.Floor
	if every <= 0 {
		every = waitFloor
	}

	floor := time.NewTicker(every)
	defer floor.Stop()

	start := time.Now()

	for {
		rows, err := mod.db.ListMessages(owner, req.Query)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}

		select {
		case <-woke:
		case <-floor.C:
			// why the report rides the floor and not the wake: a wake ends the
			// park on the next look, and the floor is the only tick that
			// repeats while nothing arrives.
			if req.Report != nil {
				req.Report(time.Since(start), req.Timeout)
			}
		case <-deadline.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
