package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
)

const (
	sessionFormatRaw     = "raw"
	sessionFormatObjects = "objects"
)

var errReceiveBusy = errors.New("receive already in progress")

// session is one live dialog stream between an agent and a remote identity.
// A pump goroutine owns the read side of conn and delivers complete messages
// into ch, so decode state never depends on tool-call timing.
type session struct {
	id      string
	agent   string // agent identity string; key for sweeps
	conn    astral.Conn
	caller  *astral.Identity
	path    string
	params  map[string]string
	format  string
	payload []byte // initial request bytes, reported by astral-listen

	ch        chan []byte   // one message per element; closed by the pump on EOF
	mu        sync.Mutex    // serializes writes to conn
	ttl       time.Duration // idle expiry window
	timer     *time.Timer   // idle expiry; reset on send/receive
	receiving atomic.Bool
}

type sessionInfo struct {
	agent  *astral.Identity
	conn   astral.Conn
	caller *astral.Identity
	path   string
	params map[string]string
	format string
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// newSession registers a live conn as a dialog session and starts its pump.
func (mod *Module) newSession(info sessionInfo) *session {
	s := &session{
		id:     newSessionID(),
		agent:  info.agent.String(),
		conn:   info.conn,
		caller: info.caller,
		path:   info.path,
		params: info.params,
		format: info.format,
		ch:     make(chan []byte, 64),
		ttl:    mod.config.SessionTTL,
	}
	s.timer = time.AfterFunc(s.ttl, func() { mod.closeSession(s.id) })
	mod.sessions.Set(s.id, s)
	go s.pump()
	return s
}

// closeSession closes the conn and forgets the session; safe to call twice.
func (mod *Module) closeSession(id string) {
	s, ok := mod.sessions.Delete(id)
	if !ok {
		return
	}
	s.timer.Stop()
	s.conn.Close()
	// why: the pump may be blocked sending into ch; drain until it exits.
	go func() {
		for range s.ch {
		}
	}()
}

// closeAgentSessions closes every session belonging to the agent.
func (mod *Module) closeAgentSessions(agentID *astral.Identity) {
	key := agentID.String()
	for id, s := range mod.sessions.Clone() {
		if s.agent == key {
			mod.closeSession(id)
		}
	}
}

// pump owns conn reads: raw chunks or decoded objects land in ch as complete
// messages. Closing conn is the only way to stop it.
func (s *session) pump() {
	defer close(s.ch)

	if s.format == sessionFormatObjects {
		recv := channel.NewReceiver(s.conn, channel.AllowUnparsed(true))
		for {
			obj, err := recv.Receive()
			if err != nil {
				return
			}
			if _, ok := obj.(*astral.EOS); ok {
				return
			}
			msg, err := objectJSON(obj)
			if err != nil {
				// note: an object with no JSON form is dropped, not fatal
				continue
			}
			s.ch <- msg
		}
	}

	buf := make([]byte, 4096)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			s.ch <- append([]byte(nil), buf[:n]...)
		}
		if err != nil {
			return
		}
	}
}

// touch pushes the idle expiry a full window into the future.
func (s *session) touch() {
	s.timer.Reset(s.ttl)
}

// send writes data to the session and refreshes its idle deadline.
func (s *session) send(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touch()
	return s.conn.Write(data)
}

// receive waits up to timeout for the next message, then greedily drains what
// is already buffered, bounded by maxBytes and maxMsgs. closed reports the
// remote end has ended the stream. Refreshes the idle deadline.
func (s *session) receive(ctx context.Context, timeout time.Duration, maxBytes, maxMsgs int) (msgs [][]byte, closed bool, err error) {
	if !s.receiving.CompareAndSwap(false, true) {
		return nil, false, errReceiveBusy
	}
	defer s.receiving.Store(false)

	// why: keep the session alive through the wait, then restart a full
	// idle window when the receive ends.
	s.timer.Reset(timeout + s.ttl)
	defer s.touch()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var total int

	select {
	case msg, ok := <-s.ch:
		if !ok {
			return nil, true, nil
		}
		msgs = append(msgs, msg)
		total += len(msg)
	case <-timer.C:
		return nil, false, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}

	for total < maxBytes && len(msgs) < maxMsgs {
		select {
		case msg, ok := <-s.ch:
			if !ok {
				return msgs, true, nil
			}
			msgs = append(msgs, msg)
			total += len(msg)
		default:
			return msgs, false, nil
		}
	}

	return msgs, false, nil
}

// objectJSON renders an object in the canonical JSON envelope {Type, Object}.
func objectJSON(obj astral.Object) ([]byte, error) {
	if u, ok := obj.(*astral.UnparsedObject); ok {
		return json.Marshal(map[string]any{
			"Type":    u.Type,
			"Payload": u.Payload, // marshals as base64
		})
	}
	payload, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return json.Marshal(&astral.JSONAdapter{Type: obj.ObjectType(), Object: payload})
}
