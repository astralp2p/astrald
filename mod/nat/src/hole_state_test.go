package nat

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	natmod "github.com/astralp2p/astrald/mod/nat"
)

// note: A is active at 10.0.0.1:40000 and B passive at 10.0.0.2:40001, matching NewPipePacketPair defaults.
type holeFixture struct {
	a, b         *astral.Identity
	spec         nat.Hole
	connA, connB net.PacketConn
}

func newHoleFixture(t *testing.T) holeFixture {
	t.Helper()

	a, b := astral.GenerateIdentity(), astral.GenerateIdentity()
	connA, connB := NewPipePacketPair(nil, nil, nil, nil)
	t.Cleanup(func() {
		_ = connA.Close()
		_ = connB.Close()
	})

	return holeFixture{
		a: a,
		b: b,
		spec: nat.Hole{
			ActiveIdentity:  a,
			ActiveEndpoint:  nat.Endpoint{IP: ip.IP(net.ParseIP("10.0.0.1")), Port: 40000},
			PassiveIdentity: b,
			PassiveEndpoint: nat.Endpoint{IP: ip.IP(net.ParseIP("10.0.0.2")), Port: 40001},
		},
		connA: connA,
		connB: connB,
	}
}

// why: keepalive is never started, so every transition runs synchronously in the test goroutine.
func (f holeFixture) holeA(opts ...HoleOption) *Hole {
	return NewHoleWithConn(f.spec, f.a, true, f.connA, opts...)
}

// why: memPacketConn.ReadFrom blocks on an empty queue, so emptiness is read from the queue itself.
func queued(t *testing.T, conn net.PacketConn) int {
	t.Helper()

	mc, ok := conn.(*memPacketConn)
	if !ok {
		t.Fatalf("conn is %T; want *memPacketConn", conn)
	}

	mc.mu.Lock()
	defer mc.mu.Unlock()
	return len(mc.queue)
}

func waitCtx(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestHoleNewIsIdle(t *testing.T) {
	h := newHoleFixture(t).holeA()

	if got := h.State(); got != StateIdle {
		t.Errorf("State() = %v; want %v", got, StateIdle)
	}
	if !h.IsIdle() {
		t.Error("IsIdle() = false; want true")
	}
}

func TestHoleBeginLockOnlyOnce(t *testing.T) {
	h := newHoleFixture(t).holeA()

	if !h.BeginLock() {
		t.Fatal("first BeginLock() = false; want true")
	}
	if got := h.State(); got != StateInLocking {
		t.Errorf("State() = %v; want %v", got, StateInLocking)
	}
	if h.BeginLock() {
		t.Error("second BeginLock() = true; want false")
	}
}

func TestHoleFinalizeLockClosesConn(t *testing.T) {
	f := newHoleFixture(t)
	h := f.holeA()

	h.BeginLock()
	if !h.finalizeLock() {
		t.Fatal("finalizeLock() = false; want true")
	}
	if !h.IsLocked() {
		t.Errorf("IsLocked() = false; state %v", h.State())
	}
	if err := h.WaitLocked(waitCtx(t)); err != nil {
		t.Errorf("WaitLocked() = %v; want nil", err)
	}
	if _, err := f.connA.WriteTo([]byte{1}, nil); !errors.Is(err, net.ErrClosed) {
		t.Errorf("connA.WriteTo after lock = %v; want %v", err, net.ErrClosed)
	}
	if h.finalizeLock() {
		t.Error("second finalizeLock() = true; want false")
	}
}

func TestHoleExpireNotifiesOnce(t *testing.T) {
	var expired []*Hole
	h := newHoleFixture(t).holeA(WithOnHoleExpire(func(h *Hole) {
		expired = append(expired, h)
	}))

	h.Expire()

	if !h.IsExpired() {
		t.Errorf("IsExpired() = false; state %v", h.State())
	}
	if len(expired) != 1 || expired[0] != h {
		t.Errorf("expire callback got %v; want exactly [%p]", expired, h)
	}
	if err := h.WaitLocked(waitCtx(t)); !errors.Is(err, natmod.ErrHoleCantLock) {
		t.Errorf("WaitLocked() = %v; want %v", err, natmod.ErrHoleCantLock)
	}
	if h.BeginLock() {
		t.Error("BeginLock() on expired hole = true; want false")
	}
}

func TestHoleWaitLockedHonorsContext(t *testing.T) {
	h := newHoleFixture(t).holeA()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := h.WaitLocked(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("WaitLocked(cancelled) = %v; want %v", err, context.Canceled)
	}
}

func TestHoleHandlePingAnswersWithPong(t *testing.T) {
	f := newHoleFixture(t)
	h := f.holeA()

	before := time.Now().Add(-time.Hour)
	h.lastPing.Store(before.UnixNano())

	h.handlePing(pingEvent{nonce: 7, pong: false})

	if n := queued(t, f.connB); n != 1 {
		t.Fatalf("connB has %d queued datagrams; want 1 pong", n)
	}

	buf := make([]byte, 64)
	n, _, err := f.connB.ReadFrom(buf)
	if err != nil {
		t.Fatalf("connB.ReadFrom: %v", err)
	}
	if n != 9 {
		t.Fatalf("pong is %d bytes; want 9", n)
	}

	var frame pingFrame
	if _, err := frame.ReadFrom(bytes.NewReader(buf[:n])); err != nil {
		t.Fatalf("decode pong: %v", err)
	}
	if want := (pingFrame{Nonce: 7, Pong: true}); frame != want {
		t.Errorf("pong frame = %+v; want %+v", frame, want)
	}
	if !h.LastPing().After(before) {
		t.Errorf("LastPing() = %v; want after %v", h.LastPing(), before)
	}
}

func TestHoleMatchingPongClearsPing(t *testing.T) {
	h := newHoleFixture(t).holeA()

	if err := h.sendPing(); err != nil {
		t.Fatalf("sendPing: %v", err)
	}
	if n := h.pings.Len(); n != 1 {
		t.Fatalf("pings.Len() after sendPing = %d; want 1", n)
	}

	var nonce astral.Nonce
	for k := range h.pings.Clone() {
		nonce = k
	}

	h.handlePing(pingEvent{nonce: nonce, pong: true})

	if n := h.pings.Len(); n != 0 {
		t.Errorf("pings.Len() after matching pong = %d; want 0", n)
	}
}

func TestHoleLockedIgnoresPing(t *testing.T) {
	f := newHoleFixture(t)
	h := f.holeA()

	h.BeginLock()
	h.finalizeLock()

	before := time.Now().Add(-time.Hour)
	h.lastPing.Store(before.UnixNano())

	h.handlePing(pingEvent{nonce: 7, pong: false})

	if got := h.LastPing(); !got.Equal(before) {
		t.Errorf("LastPing() = %v; want unchanged %v", got, before)
	}
	if n := queued(t, f.connB); n != 0 {
		t.Errorf("connB has %d queued datagrams; want 0", n)
	}
}

func TestHoleExpirePingsUsesLifespan(t *testing.T) {
	h := newHoleFixture(t).holeA(WithPingLifespan(time.Hour))

	const fresh, stale astral.Nonce = 1, 2
	h.pings.Set(fresh, time.Now().UnixNano())
	h.pings.Set(stale, time.Now().Add(-2*time.Hour).UnixNano())

	h.expirePings()

	if _, ok := h.pings.Get(fresh); !ok {
		t.Error("fresh ping was expired; want kept")
	}
	if _, ok := h.pings.Get(stale); ok {
		t.Error("stale ping was kept; want expired")
	}
}

func TestHoleLockTimedOutUsesLockTimeout(t *testing.T) {
	h := newHoleFixture(t).holeA(WithLockTimeout(time.Hour))

	h.BeginLock()
	if h.lockTimedOut() {
		t.Error("lockTimedOut() right after BeginLock = true; want false")
	}

	h.lockStart.Store(time.Now().Add(-2 * time.Hour).UnixNano())
	if !h.lockTimedOut() {
		t.Error("lockTimedOut() two hours after lock start = false; want true")
	}
}

func TestHoleIsExpectedAddr(t *testing.T) {
	f := newHoleFixture(t)
	holeA := f.holeA()
	holeB := NewHoleWithConn(f.spec, f.b, false, f.connB)

	udp := func(s string, port int) *net.UDPAddr {
		return &net.UDPAddr{IP: net.ParseIP(s), Port: port}
	}

	tests := []struct {
		name string
		hole *Hole
		addr *net.UDPAddr
		want bool
	}{
		{"A sees passive endpoint", holeA, udp("10.0.0.2", 40001), true},
		{"A rejects wrong port", holeA, udp("10.0.0.2", 40002), false},
		{"A rejects wrong ip", holeA, udp("10.0.0.3", 40001), false},
		{"B sees active endpoint", holeB, udp("10.0.0.1", 40000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.hole.isExpectedAddr(tt.addr); got != tt.want {
				t.Errorf("isExpectedAddr(%v) = %v; want %v", tt.addr, got, tt.want)
			}
		})
	}
}
