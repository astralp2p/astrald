package vfs

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// lockVFS takes the lock of one VFS and returns the function that releases it.
//
// Every change to a VFS runs under its lock, so changes to one VFS happen one at a time. The lock is in-process
// and covers the database part of a change only. No operation waits for another module while it holds one.
// note: a lock is never dropped, because no operation deletes a VFS in this version.
func (mod *Module) lockVFS(id *astral.Identity) (unlock func()) {
	mu, _ := mod.locks.Set(id.String(), &sync.Mutex{})
	mu.Lock()

	return mu.Unlock
}
