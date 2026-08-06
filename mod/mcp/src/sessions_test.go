package mcp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

type testConn struct {
	net.Conn
	local, remote *astral.Identity
}

func (c testConn) LocalIdentity() *astral.Identity  { return c.local }
func (c testConn) RemoteIdentity() *astral.Identity { return c.remote }

// testSessionModule returns a module with short timing plus both ends of a
// session conn: the session wraps one end, peer is the other.
func testSessionModule(t *testing.T) (mod *Module, s *session, peer net.Conn) {
	t.Helper()

	mod = &Module{config: Config{
		SessionTTL:         100 * time.Millisecond,
		PayloadReadWindow:  30 * time.Millisecond,
		MaxPayloadBytes:    64 << 10,
		MaxResponseBytes:   64 << 10,
		MaxResponseObjects: 64,
	}}

	local, remote := net.Pipe()
	agentID := astral.GenerateIdentity()

	s = mod.newSession(sessionInfo{
		agent:  agentID,
		conn:   testConn{Conn: local, local: agentID, remote: astral.GenerateIdentity()},
		caller: astral.GenerateIdentity(),
		format: sessionFormatRaw,
	})

	return mod, s, remote
}

func TestSessionReceive(t *testing.T) {
	_, s, peer := testSessionModule(t)

	go peer.Write([]byte("hello"))

	msgs, closed, err := s.receive(context.Background(), time.Second, 64<<10, 64)
	if err != nil || closed {
		t.Fatalf("receive: err %v, closed %v", err, closed)
	}
	if len(msgs) != 1 || string(msgs[0]) != "hello" {
		t.Fatalf("received %q", msgs)
	}
}

func TestSessionReceiveTimeout(t *testing.T) {
	_, s, _ := testSessionModule(t)

	msgs, closed, err := s.receive(context.Background(), 30*time.Millisecond, 64<<10, 64)
	if err != nil || closed || len(msgs) != 0 {
		t.Fatalf("receive: msgs %v, closed %v, err %v", msgs, closed, err)
	}
}

func TestSessionReceiveClosed(t *testing.T) {
	_, s, peer := testSessionModule(t)

	peer.Close()

	_, closed, err := s.receive(context.Background(), time.Second, 64<<10, 64)
	if err != nil || !closed {
		t.Fatalf("receive: closed %v, err %v", closed, err)
	}
}

func TestSessionReceiveBusy(t *testing.T) {
	_, s, _ := testSessionModule(t)

	go s.receive(context.Background(), time.Second, 64<<10, 64)
	time.Sleep(10 * time.Millisecond)

	_, _, err := s.receive(context.Background(), time.Second, 64<<10, 64)
	if !errors.Is(err, errReceiveBusy) {
		t.Fatalf("second receive: got %v, want errReceiveBusy", err)
	}
}

func TestSessionSend(t *testing.T) {
	_, s, peer := testSessionModule(t)

	buf := make([]byte, 16)
	done := make(chan int, 1)
	go func() {
		n, _ := peer.Read(buf)
		done <- n
	}()

	if _, err := s.send([]byte("hi")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if n := <-done; string(buf[:n]) != "hi" {
		t.Fatalf("peer read %q", buf[:n])
	}
}

func TestSessionIdleExpiry(t *testing.T) {
	mod, s, _ := testSessionModule(t)

	time.Sleep(250 * time.Millisecond)

	if _, ok := mod.sessions.Get(s.id); ok {
		t.Fatal("session still tracked after idle expiry")
	}
	if _, err := s.conn.Write([]byte("x")); err == nil {
		t.Fatal("conn still writable after idle expiry")
	}
}

func TestCloseSessionIdempotent(t *testing.T) {
	mod, s, _ := testSessionModule(t)

	mod.closeSession(s.id)
	mod.closeSession(s.id)

	if _, ok := mod.sessions.Get(s.id); ok {
		t.Fatal("session still tracked after close")
	}
}

func TestCloseAgentSessions(t *testing.T) {
	mod, s, _ := testSessionModule(t)

	otherID := astral.GenerateIdentity()
	local, _ := net.Pipe()
	other := mod.newSession(sessionInfo{
		agent:  otherID,
		conn:   testConn{Conn: local, local: otherID, remote: astral.GenerateIdentity()},
		caller: astral.GenerateIdentity(),
		format: sessionFormatRaw,
	})

	mod.closeAgentSessions(otherID)

	if _, ok := mod.sessions.Get(other.id); ok {
		t.Fatal("agent session still tracked after sweep")
	}
	if _, ok := mod.sessions.Get(s.id); !ok {
		t.Fatal("unrelated session swept")
	}
}
