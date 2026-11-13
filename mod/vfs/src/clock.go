package vfs

import "time"

// clock is the time the background compute and the sweeper run on.
//
// why: the timing rules of the background compute are tested on a clock the test moves.
// note: the times of entries come from nodeTime, not from this clock.
type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) clockTimer
}

// clockTimer fires once on C after its duration. A duration of 0 or below fires at once.
type clockTimer interface {
	C() <-chan time.Time
	Stop() bool
}

// systemClock is the clock of the node.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) NewTimer(d time.Duration) clockTimer { return systemTimer{time.NewTimer(d)} }

type systemTimer struct{ t *time.Timer }

func (t systemTimer) C() <-chan time.Time { return t.t.C }

func (t systemTimer) Stop() bool { return t.t.Stop() }
