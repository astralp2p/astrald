package mcp

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

var errAlreadyListening = errors.New("already listening")

// pendingQuery is an accepted inbound query waiting for the agent's next
// astral-listen call.
type pendingQuery struct {
	session *session
	timer   *time.Timer
}

// parkListener registers a one-shot listener for queries targeting agentID.
// The returned channel delivers at most one session. The caller must
// unparkListener when done, whether or not a session arrived.
func (mod *Module) parkListener(agentID *astral.Identity) (chan *session, error) {
	mod.listenMu.Lock()
	defer mod.listenMu.Unlock()

	key := agentID.String()
	if mod.listeners == nil {
		mod.listeners = map[string]chan *session{}
	}
	if _, taken := mod.listeners[key]; taken {
		return nil, errAlreadyListening
	}

	ch := make(chan *session, 1)
	mod.listeners[key] = ch
	return ch, nil
}

// popListener atomically claims the parked listener for target, if any.
func (mod *Module) popListener(target *astral.Identity) (chan *session, bool) {
	mod.listenMu.Lock()
	defer mod.listenMu.Unlock()

	ch, ok := mod.listeners[target.String()]
	if ok {
		delete(mod.listeners, target.String())
	}
	return ch, ok
}

// unparkListener removes ch if it is still parked and closes a session that
// raced in after the listener gave up waiting.
func (mod *Module) unparkListener(agentID *astral.Identity, ch chan *session) {
	key := agentID.String()

	mod.listenMu.Lock()
	// why: only remove our own channel — a fresh listen may have parked a new
	// one after a query claimed ours.
	if cur, ok := mod.listeners[key]; ok && cur == ch {
		delete(mod.listeners, key)
	}
	mod.listenMu.Unlock()

	select {
	case s := <-ch:
		if s != nil {
			mod.closeSession(s.id)
		}
	default:
	}
}

// drainListener drops whatever listener the agent has parked.
func (mod *Module) drainListener(agentID *astral.Identity) {
	if ch, ok := mod.popListener(agentID); ok {
		select {
		case s := <-ch:
			if s != nil {
				mod.closeSession(s.id)
			}
		default:
		}
	}
}

// pendingCount reports how many queries wait for the agent.
func (mod *Module) pendingCount(agentID *astral.Identity) int {
	mod.listenMu.Lock()
	defer mod.listenMu.Unlock()
	return len(mod.pending[agentID.String()])
}

// enqueuePending holds an accepted query for the agent's next listen call.
// A full queue closes the session — the caller sees the stream end.
func (mod *Module) enqueuePending(agentID *astral.Identity, s *session) {
	key := agentID.String()

	mod.listenMu.Lock()
	if mod.pending == nil {
		mod.pending = map[string][]*pendingQuery{}
	}
	if len(mod.pending[key]) >= mod.config.MaxPending {
		mod.listenMu.Unlock()
		mod.closeSession(s.id)
		return
	}
	p := &pendingQuery{session: s}
	p.timer = time.AfterFunc(mod.config.PendingTTL, func() {
		if mod.removePending(key, s) {
			mod.closeSession(s.id)
		}
	})
	mod.pending[key] = append(mod.pending[key], p)
	mod.listenMu.Unlock()
}

// takePending claims the oldest query waiting for the agent.
func (mod *Module) takePending(agentID *astral.Identity) (*session, bool) {
	mod.listenMu.Lock()
	defer mod.listenMu.Unlock()

	key := agentID.String()
	queue := mod.pending[key]
	if len(queue) == 0 {
		return nil, false
	}

	p := queue[0]
	if len(queue) == 1 {
		delete(mod.pending, key)
	} else {
		mod.pending[key] = queue[1:]
	}

	p.timer.Stop()
	p.session.touch()
	return p.session, true
}

// removePending drops a specific queued query, reporting whether it was still
// queued — the expiry timer closes the session only when it wins that race.
func (mod *Module) removePending(key string, s *session) bool {
	mod.listenMu.Lock()
	defer mod.listenMu.Unlock()

	queue := mod.pending[key]
	for i, p := range queue {
		if p.session != s {
			continue
		}
		queue = append(queue[:i], queue[i+1:]...)
		if len(queue) == 0 {
			delete(mod.pending, key)
		} else {
			mod.pending[key] = queue
		}
		return true
	}
	return false
}

// dropPending discards every queued query for the agent.
func (mod *Module) dropPending(agentID *astral.Identity) {
	key := agentID.String()

	mod.listenMu.Lock()
	queue := mod.pending[key]
	delete(mod.pending, key)
	mod.listenMu.Unlock()

	for _, p := range queue {
		p.timer.Stop()
		mod.closeSession(p.session.id)
	}
}
