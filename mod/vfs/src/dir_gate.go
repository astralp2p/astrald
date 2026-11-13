package vfs

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// dirGate keeps the store and the delete of one directory ObjectID apart.
//
// A store holds its ID from before its commit to the end of its write-back. The sweeper deletes an ID only while
// no store holds it, and a store of an ID waits while the sweeper deletes it.
//
// why: a write-back runs only inside a store window, so once a delete began, no row takes the ID until it ends,
// and the holder check before the delete stays true.
// note: the gate is not held across IO by both sides: the sweeper never waits, and a store waits for one delete.
// note: HoldObject never takes the gate, so a purge that asks the holders never waits for it.
// why: a mutex guards the maps, because a store waits on a condition over them.
type dirGate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	stores   map[astral.ObjectID]int
	deleting map[astral.ObjectID]bool
}

func newDirGate() *dirGate {
	g := &dirGate{stores: map[astral.ObjectID]int{}, deleting: map[astral.ObjectID]bool{}}
	g.cond = sync.NewCond(&g.mu)

	return g
}

// beginStore waits until no delete of the ID runs, and holds the ID for a store.
func (g *dirGate) beginStore(id astral.ObjectID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	for g.deleting[id] {
		g.cond.Wait()
	}
	g.stores[id]++
}

// endStore releases the ID held by beginStore.
func (g *dirGate) endStore(id astral.ObjectID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.stores[id]--; g.stores[id] <= 0 {
		delete(g.stores, id)
	}
}

// tryBeginDelete holds the ID for a delete, or reports false while a store holds it.
func (g *dirGate) tryBeginDelete(id astral.ObjectID) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.stores[id] > 0 || g.deleting[id] {
		return false
	}
	g.deleting[id] = true

	return true
}

// endDelete releases the ID held by tryBeginDelete and wakes the stores that wait for it.
func (g *dirGate) endDelete(id astral.ObjectID) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.deleting, id)
	g.cond.Broadcast()
}
