package mcp

import (
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

var errAlreadyListening = errors.New("already listening")

// note: a session can slip past the drains here when RouteQuery has claimed a
// listener but not yet delivered into it; such orphans expire via SessionTTL.

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

	// wake queries waiting out a listen gap
	if mod.parked != nil {
		close(mod.parked)
		mod.parked = nil
	}

	return ch, nil
}

// awaitListener waits up to grace for a listener to park for target,
// bridging the gap between an agent's listen polls.
func (mod *Module) awaitListener(ctx *astral.Context, target *astral.Identity, grace time.Duration) (chan *session, bool) {
	deadline := time.NewTimer(grace)
	defer deadline.Stop()

	for {
		// why: snapshot the park signal before trying the pop — a park
		// landing in between still wakes the wait below.
		mod.listenMu.Lock()
		if mod.parked == nil {
			mod.parked = make(chan struct{})
		}
		signal := mod.parked
		mod.listenMu.Unlock()

		if ch, ok := mod.popListener(target); ok {
			return ch, true
		}

		select {
		case <-signal:
		case <-deadline.C:
			return nil, false
		case <-ctx.Done():
			return nil, false
		}
	}
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
