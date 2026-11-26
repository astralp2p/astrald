package user

import (
	"context"
	"time"
)

// swarmPollInterval is how often a swarmWatcher reads SwarmHead.
const swarmPollInterval = 3 * time.Second

// swarmHeadReader is the part of nodesmod.SwarmState a swarmWatcher reads.
type swarmHeadReader interface {
	SwarmHead() uint64
}

// swarmWatcher calls onChange with the new head each time SwarmHead differs
// from the last head it saw. The set only grows, so an unchanged head means
// nothing new, and a changed one means entries arrived.
//
// todo: switch to vfs.watch on swarmstate/ once it lands (VFS takes over from
// tree); only this reader side changes.
type swarmWatcher struct {
	state    swarmHeadReader
	interval time.Duration
	onChange func(head uint64)
}

// newSwarmWatcher returns a watcher of state; a non-positive interval selects
// swarmPollInterval.
func newSwarmWatcher(state swarmHeadReader, interval time.Duration, onChange func(head uint64)) *swarmWatcher {
	if interval <= 0 {
		interval = swarmPollInterval
	}
	return &swarmWatcher{state: state, interval: interval, onChange: onChange}
}

// Run polls until ctx ends. Its first poll runs at once, so a non-empty set is
// reported on start.
func (w *swarmWatcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	var last uint64
	for {
		if head := w.state.SwarmHead(); head != last {
			last = head
			w.onChange(head)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
