package nodes

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// basicRoundBudget bounds one round of the basic strategy.
const basicRoundBudget = 60 * time.Second

// natRoundBudget bounds one round of the NAT strategy.
const natRoundBudget = 60 * time.Second

// linkRound is one shared run of a link strategy toward one target.
//
// A round runs on a context derived from the module context with a budget. The
// context ends when every caller that joined the round has released it, when
// the budget ends, or when the module stops. Its done channel closes when the
// round finishes.
type linkRound struct {
	keeper *roundKeeper
	ctx    *astral.Context
	cancel context.CancelFunc
	done   chan struct{}
	closed atomic.Bool

	refs      int  // guarded by keeper.mu
	abandoned bool // guarded by keeper.mu: refs reached zero before ctx ended
}

// roundKeeper holds the running round of one strategy.
type roundKeeper struct {
	mu    sync.Mutex
	round *linkRound
}

// join adds a caller to the running round, or starts a new round on parent
// with budget and runs run on it in a new goroutine. The round finishes when
// run returns or run calls finished. A round whose context has ended takes no
// new callers; join starts a new round instead.
//
// done is the done channel of the round the caller joined; it closes when that
// round finishes, never when a later round does.
//
// The caller holds the round until it calls the returned release or caller
// ends, whichever comes first. Release is idempotent.
func (k *roundKeeper) join(caller, parent *astral.Context, budget time.Duration, run func(*linkRound)) (done <-chan struct{}, release func()) {
	r := k.enter(parent, budget, run)

	var left atomic.Bool
	leave := func() {
		if left.CompareAndSwap(false, true) {
			r.leave()
		}
	}

	// why: a caller that never calls release still leaves when its context ends,
	// so a missed release cannot hold a round past its callers.
	stop := context.AfterFunc(caller, leave)

	return r.done, func() {
		stop()
		leave()
	}
}

// enter counts one more caller of the running round, starting a new round when none takes callers.
func (k *roundKeeper) enter(parent *astral.Context, budget time.Duration, run func(*linkRound)) *linkRound {
	k.mu.Lock()
	defer k.mu.Unlock()

	r := k.round
	if r == nil || r.ctx.Err() != nil {
		ctx, cancel := parent.WithTimeout(budget)
		r = &linkRound{keeper: k, ctx: ctx, cancel: cancel, done: make(chan struct{})}
		k.round = r
		go func() {
			defer r.finished()
			defer cancel()
			run(r)
		}()
	}
	r.refs++
	return r
}

// leave drops one caller; the last caller to leave cancels the round.
func (r *linkRound) leave() {
	r.keeper.mu.Lock()
	defer r.keeper.mu.Unlock()

	r.refs--
	if r.refs > 0 {
		return
	}
	if r.ctx.Err() == nil {
		r.abandoned = true
	}
	r.cancel()
}

// finished closes the round's done channel and detaches it from its keeper,
// so the next join starts a new round. Calls after the first do nothing.
func (r *linkRound) finished() {
	if !r.closed.CompareAndSwap(false, true) {
		return
	}

	r.keeper.mu.Lock()
	defer r.keeper.mu.Unlock()

	if r.keeper.round == r {
		r.keeper.round = nil
	}
	close(r.done)
}

// wasAbandoned reports whether every caller left before the round's context ended.
func (r *linkRound) wasAbandoned() bool {
	r.keeper.mu.Lock()
	defer r.keeper.mu.Unlock()
	return r.abandoned
}
