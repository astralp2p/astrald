package user

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// swarmPollInterval is how often the head feed reads SwarmHead.
const swarmPollInterval = 3 * time.Second

// SwarmFeed reports changes of this node's SwarmState. It is the one place
// change detection lives: wireSwarm picks the implementation.
type SwarmFeed interface {
	// Follow returns a channel that receives a SwarmChange after the set
	// changed, and is closed when ctx ends.
	Follow(ctx *astral.Context) <-chan SwarmChange
}

// SwarmChange is one change of the set. IDs names the entries that arrived;
// nil means the reader re-reads the set.
type SwarmChange struct{ IDs []*astral.ObjectID }

// headFeed reports a change each time SwarmHead differs from the last head it
// saw. The set only grows, so an unchanged head means nothing new, and a
// changed one means entries arrived.
//
// note: polling the head stands in until a feed over vfs.watch on swarmstate/
// lands (VFS takes over from tree); only the line in wireSwarm that picks the
// feed changes.
type headFeed struct {
	head     func() uint64
	interval time.Duration
}

var _ SwarmFeed = &headFeed{}

// Follow polls until ctx ends. Its first poll runs at once, so a non-empty set
// is reported on start. A change carries no IDs.
func (f *headFeed) Follow(ctx *astral.Context) <-chan SwarmChange {
	changes := make(chan SwarmChange)
	go func() {
		defer close(changes)
		ticker := time.NewTicker(f.interval)
		defer ticker.Stop()

		var last uint64
		for {
			if head := f.head(); head != last {
				last = head
				select {
				case changes <- SwarmChange{}:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return changes
}
