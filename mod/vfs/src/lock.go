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

// runLockedTx runs fn in one transaction under the lock of a VFS. The lock is taken before the transaction.
//
// note: fn calls no other module and no repository; the node has one database connection.
func (mod *Module) runLockedTx(id *astral.Identity, fn func(tx *DB) error) error {
	return mod.runLockedTxThen(id, fn, nil)
}

// runLockedTxThen is runLockedTx that calls after once the transaction committed, still under the lock. A nil
// after is skipped.
//
// note: after takes no lock and does no IO; it updates the in-memory state of the background compute.
func (mod *Module) runLockedTxThen(id *astral.Identity, fn func(tx *DB) error, after func()) error {
	unlock := mod.lockVFS(id)
	defer unlock()

	if err := mod.db.inTx(fn); err != nil {
		return err
	}

	if after != nil {
		after()
	}

	return nil
}
