package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// waitFloor is how often a parked wait looks again on its own, under the wake.
//
// why a floor under the wake: a wake fires only where a writer calls it, and a
// missed one makes the tool report timed_out over an inbox holding unarchived
// mail. Ten seconds sits under Config.WaitDefault, so an unnamed park catches a
// missed wake before its own deadline.
const waitFloor = 10 * time.Second

// pollMessages parks until the owner's list holds a message the query matches,
// and answers what it found. Nothing is stamped and nothing is taken, so two
// agents waiting at once are answered the same messages.
func (mod *Module) pollMessages(ctx context.Context, owner *astral.Identity, q messageQuery, timeout time.Duration) ([]dbMessage, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	// why the registration precedes the first look: a row landing between the
	// query and the subscribe signals into a registry this waiter is not yet
	// in, so the token is never made. The buffered channel closes the other
	// window; neither alone is enough.
	woke, leave := mod.waiters.park(owner)
	defer leave()

	floor := time.NewTicker(waitFloor)
	defer floor.Stop()

	for {
		rows, err := mod.db.ListMessages(owner, q)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}

		select {
		case <-woke:
		case <-floor.C:
		case <-deadline.C:
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
