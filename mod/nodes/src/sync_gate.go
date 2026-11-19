package nodes

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// syncGate runs at most one sync per peer at a time.
// A request while the peer's sync runs folds into one trailing rerun.
// why a rerun and not a drop: state that changed during the running sync is
// still sent once after it.
// note: the zero value is ready to use.
type syncGate struct {
	// why a mutex and not sig.Map: the rerun check and the idle transition are
	// one step, or a request between them is lost.
	mu sync.Mutex
	// rerun holds a key per peer whose sync runs; the value is a pending rerun.
	rerun map[string]bool
}

// Run starts fn for id unless a sync for id runs; then it requests one rerun.
// note: a rerun calls the fn of the call that started the run, so every fn
// passed for one id does the same work.
func (g *syncGate) Run(id *astral.Identity, fn func()) {
	key := id.String()

	g.mu.Lock()
	defer g.mu.Unlock()

	if _, running := g.rerun[key]; running {
		g.rerun[key] = true
		return
	}
	if g.rerun == nil {
		g.rerun = make(map[string]bool)
	}
	g.rerun[key] = false

	go g.loop(key, fn)
}

// loop runs fn until no rerun is pending for key.
func (g *syncGate) loop(key string, fn func()) {
	for {
		fn()
		if !g.next(key) {
			return
		}
	}
}

// next consumes a pending rerun for key and reports whether there was one.
// Without one, key goes idle.
func (g *syncGate) next(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.rerun[key] {
		g.rerun[key] = false
		return true
	}
	delete(g.rerun, key)
	return false
}
