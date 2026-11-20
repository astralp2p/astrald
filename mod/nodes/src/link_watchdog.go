package nodes

import (
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// watchedConn is a link conn that calls onStall when one Write stays blocked for timeout.
// why on the conn: the mux writes frames through the link's channel, not through Link.Send.
// why one timer: the channel's write lock admits one Write at a time, and a frame is one Write.
type watchedConn struct {
	astral.Conn
	timeout time.Duration
	onStall func()
	base    time.Time        // clock origin, read once; offsets from it use the monotonic clock
	now     func() time.Time // time.Now; a test replaces it
	since   atomic.Int64     // 1 + offset from base at which the Write in flight started; 0 while none is in flight
	wdog    *time.Timer
}

func newWatchedConn(conn astral.Conn, timeout time.Duration, onStall func()) *watchedConn {
	c := &watchedConn{Conn: conn, timeout: timeout, onStall: onStall, now: time.Now}
	c.base = c.now()
	c.wdog = time.AfterFunc(timeout, c.check)
	c.wdog.Stop()
	return c
}

func (c *watchedConn) Write(p []byte) (int, error) {
	c.since.Store(c.offset() + 1)
	c.wdog.Reset(c.timeout)
	n, err := c.Conn.Write(p)
	c.since.Store(0)
	c.wdog.Stop()
	return n, err
}

// offset returns the time since base. Both readings carry the monotonic clock,
// so a wall-clock step does not change it.
func (c *watchedConn) offset() int64 {
	return int64(c.now().Sub(c.base))
}

// elapsed returns how long the Write that stored since has been in flight.
func (c *watchedConn) elapsed(since int64) time.Duration {
	return time.Duration(c.offset() - (since - 1))
}

// check calls onStall when the Write in flight has been blocked for timeout.
// why the start time: a timer that fires as one Write ends and the next begins
// re-arms for the rest of the new Write's timeout instead of closing a link that moves.
func (c *watchedConn) check() {
	since := c.since.Load()
	if since == 0 {
		return
	}
	if left := c.timeout - c.elapsed(since); left > 0 {
		c.wdog.Reset(left)
		return
	}
	c.onStall()
}
