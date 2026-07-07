package scheduler

import (
	"sync"

	"github.com/cryptopunkscc/astral-go/astral/sig"
)

// PoolLocker is a scheduler dependency that locks and releases items in a sig.Pool
type PoolLocker struct {
	pool       *sig.Pool
	items      []string
	done       chan struct{}
	lockOnce   sync.Once
	unlockOnce sync.Once
}

var _ Done = &PoolLocker{}
var _ Releaser = &PoolLocker{}

// LockPool returns a new PoolLocker that locks given items in the pool on the first call to Done()
func LockPool(pool *sig.Pool, items ...string) *PoolLocker {
	return &PoolLocker{
		pool:  pool,
		items: items,
		done:  make(chan struct{}),
	}
}

// Release releases the locked items
func (p *PoolLocker) Release() {
	// release only once
	p.unlockOnce.Do(func() {
		p.pool.Unlock(p.items...)
	})
}

// Done returns a channel that closes once all pool items are locked.
// The first call spawns the locking goroutine; subsequent calls return the same channel.
func (p *PoolLocker) Done() <-chan struct{} {
	// when Done is called for the first time, spawn the locking goroutine
	p.lockOnce.Do(func() {
		go func() {
			p.pool.Lock(p.items...)
			close(p.done)
		}()
	})

	return p.done
}
