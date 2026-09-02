package mcp

import (
	"context"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// waitFloor is how often a parked wait looks again on its own.
//
// why it exists at all, given the wake: the wake is only as good as the set of
// statements that remember to fire it, and that set grows — a repair, an
// import, a tool not yet written. Without a floor a missed wake is not a slow
// answer, it is a wrong one: pollMessages returns nil on its deadline and the
// tool reports timed_out, so an agent is told the window closed with nothing
// new while unarchived mail sits in its inbox. That is the one answer this
// design exists to make impossible.
//
// why ten seconds: as the way a waiter normally learns, 250ms was right and
// cost 0.03% of a core per parked agent. As a backstop it is forty times more
// idle work than the job needs. What the interval has to satisfy is that it is
// comfortably under the default window (Config.WaitDefault), so an unnamed
// park catches a missed wake before its deadline can report timed_out over a
// non-empty inbox. A park the caller shortened below the floor leans on the
// wake alone, which covers every writer this module has.
const waitFloor = 10 * time.Second

// waitForMessages parks until the owner's chosen list holds a message the query
// matches, and answers what it found. Nothing is stamped and nothing is taken:
// two agents waiting at once are answered the same messages, and a reader that
// stops between the answer and the work leaves the mailbox as it was.
//
// why a poll and not a wake-up: the table already holds the answer, and a
// waiter woken by the writer has to be registered, unregistered and raced
// against a delivery landing between the two. A quarter of a second is under
// what an agent takes to read one message.
func (mod *Module) pollMessages(ctx context.Context, owner *astral.Identity, q messageQuery, timeout time.Duration) ([]dbMessage, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	// why the registration precedes the first look: a row landing between the
	// query and the subscribe is one the waiter would sleep through, because
	// the writer signals into a registry this waiter is not yet in — the token
	// is not dropped, it is never made. Registering first and buffering the
	// channel close two different windows, and either one alone still loses
	// wakes.
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
