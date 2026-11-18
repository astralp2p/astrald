package apphost

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// sessionEndSpacing is the least time between two bind-session ends of one owner.
//
// why: register_handler does not dial the endpoint, so an app whose endpoint
// stays dead re-registers it on every reconnect, and its next query fails
// again. The spacing bounds those reconnects to one per owner per window.
const sessionEndSpacing = 30 * time.Second

// appBind is one apphost.bind session: the tokens it bound and how to end it.
type appBind struct {
	scope  bindScope
	cancel func()

	mu      sync.Mutex
	tokens  []astral.Nonce
	ended   bool
	pending bool // an end waits for the owner's spacing to pass
}

// end marks s ended and returns its tokens. It returns none when s had
// already ended.
func (s *appBind) end() []astral.Nonce {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return nil
	}
	s.ended = true
	return s.tokens
}

// claimEnd reserves the one session end a failed dial may request. It
// reports false when s has ended or an end is already pending.
func (s *appBind) claimEnd() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended || s.pending {
		return false
	}
	s.pending = true
	return true
}

// bindKey names the token an owner's session bound.
type bindKey struct {
	owner string
	token astral.Nonce
}

// bindRegistry maps an owner's bound token to the live session that bound it.
//
// why a mutex: a session drops its entry only while the entry still names
// that session, and that check and the delete form one step.
type bindRegistry struct {
	mu       sync.Mutex
	sessions map[bindKey]*appBind
}

func (r *bindRegistry) put(owner *astral.Identity, token astral.Nonce, s *appBind) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.sessions == nil {
		r.sessions = map[bindKey]*appBind{}
	}
	r.sessions[bindKey{owner.String(), token}] = s
}

func (r *bindRegistry) find(owner *astral.Identity, token astral.Nonce) *appBind {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.sessions[bindKey{owner.String(), token}]
}

// drop deletes the entry for owner and token when it still names s.
//
// note: a reconnecting app binds the same token from a new session, which
// replaces the entry before the old session's cleanup runs.
func (r *bindRegistry) drop(owner *astral.Identity, token astral.Nonce, s *appBind) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := bindKey{owner.String(), token}
	if r.sessions[key] == s {
		delete(r.sessions, key)
	}
}

// endSpacer spaces the bind-session ends of each owner by sessionEndSpacing.
// A nil now or afterFunc selects time.Now or time.AfterFunc.
type endSpacer struct {
	mu        sync.Mutex
	next      map[string]time.Time // owner -> earliest time of its next end
	now       func() time.Time
	afterFunc func(time.Duration, func())
}

// reserve books owner's next end and returns how long that end waits.
func (e *endSpacer) reserve(owner *astral.Identity) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	if e.now != nil {
		now = e.now()
	}

	if e.next == nil {
		e.next = map[string]time.Time{}
	}
	for k, t := range e.next {
		if !t.After(now) {
			delete(e.next, k)
		}
	}

	at := now
	if next, found := e.next[owner.String()]; found {
		at = next
	}
	e.next[owner.String()] = at.Add(sessionEndSpacing)

	return at.Sub(now)
}

// schedule runs fn after wait.
func (e *endSpacer) schedule(wait time.Duration, fn func()) {
	if e.afterFunc != nil {
		e.afterFunc(wait, fn)
		return
	}
	time.AfterFunc(wait, fn)
}

// bindToken records that session s bound token.
//
// why the immediate removal: a token that reaches an ended session missed the
// session's cleanup, so its handlers are removed at once.
func (mod *Module) bindToken(s *appBind, token astral.Nonce) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		_ = mod.removeHandlersByToken(s.scope, token)
		return
	}

	s.tokens = append(s.tokens, token)
	if s.scope.owner != nil {
		mod.bindSessions.put(s.scope.owner, token, s)
	}
}

// finishBind ends session s once: it removes the handlers s bound, then
// cancels s. A second call only cancels.
//
// why the order: cancelling closes the bind channel, and a reconnecting app
// registers the same endpoint and token as soon as it sees the close. The
// handlers are removed first so that the new registration is never removed.
func (mod *Module) finishBind(s *appBind) {
	for _, token := range s.end() {
		if s.scope.owner != nil {
			mod.bindSessions.drop(s.scope.owner, token, s)
		}
		if err := mod.removeHandlersByToken(s.scope, token); err != nil {
			mod.log.Error("bind error: %v", err)
		}
	}
	s.cancel()
}

// endBindSession ends the bind session in which owner bound token, so that
// the owner's app reconnects and registers its handlers again. Ends of one
// owner are spaced by sessionEndSpacing; an end inside the window waits for
// the window to pass.
//
// note: a handler registered by a token-less session records no owner and has
// no session to end.
func (mod *Module) endBindSession(owner *astral.Identity, token astral.Nonce) {
	if owner == nil {
		return
	}

	s := mod.bindSessions.find(owner, token)
	if s == nil || !s.claimEnd() {
		return
	}

	wait := mod.sessionEnds.reserve(owner)
	if wait <= 0 {
		mod.log.Logv(1, "ending bind session of %v after its handler failed", owner)
		mod.finishBind(s)
		return
	}

	mod.log.Logv(1, "ending bind session of %v in %v after its handler failed", owner, wait)
	mod.sessionEnds.schedule(wait, func() { mod.finishBind(s) })
}
