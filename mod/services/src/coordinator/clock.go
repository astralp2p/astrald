package coordinator

import "time"

// Timer is a pending call that Stop cancels.
type Timer interface {
	Stop() bool
}

// Clock schedules calls. Tests replace it to control time.
type Clock interface {
	AfterFunc(d time.Duration, f func()) Timer
}

// SystemClock schedules calls on the runtime clock.
type SystemClock struct{}

func (SystemClock) AfterFunc(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}
