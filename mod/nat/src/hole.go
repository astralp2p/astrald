package nat

import (
	"bytes"
	"context"
	natmod "github.com/astralp2p/astrald/mod/nat"
	"net"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/nat"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

const (
	defaultPingInterval  = 1 * time.Second
	defaultNoPingTimeout = 15 * time.Second
	defaultPingLifespan  = 5 * time.Second
	defaultLockTimeout   = 10 * time.Second
	defaultMaxPingFails  = 5
)

type HoleState int32

const (
	StateIdle      HoleState = iota // normal keepalive
	StateInLocking                  // lock requested, waiting for drain
	StateLocked                     // socket silent, no traffic
	StateExpired                    // mapping corrupted / lack of reachability
)

// Hole represents a NAT-traversed UDP hole with keepalive and locking mechanisms.
// While Idle, it keeps the NAT mapping alive by sending periodic pings.
// In InLocking state, it waits for pongs to drain before completing the lock.
// Once locked, the socket is closed for handoff.
type Hole struct {
	nat.Hole
	// configuration
	localIdentity *astral.Identity
	isPinger      bool
	onHoleExpire  OnHoleExpire

	conn net.PacketConn

	// State machine
	state     atomic.Int32 // HoleState
	lockStart atomic.Int64 // unix nano
	lastPing  atomic.Int64 // unix nano
	pings     sig.Map[astral.Nonce, int64]

	// Channels (safe goroutine communication)
	pingEvents chan pingEvent // receiver → runLoop
	wakeCh     chan struct{}  // wakes runLoop for state transitions
	lockedCh   chan struct{}  // closes when locked/expired

	// configuration options
	pingInterval  time.Duration
	noPingTimeout time.Duration
	pingLifespan  time.Duration
	lockTimeout   time.Duration
	maxPingFails  int
}

// Event type from Receiver → runLoop
type pingEvent struct {
	nonce astral.Nonce
	pong  bool
}

// NewHole creates a new NAT hole with a bound UDP socket and applies the given options.
func NewHole(hole nat.Hole, localID *astral.Identity, isPinger bool, opts ...HoleOption) (*Hole, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: hole.GetLocalAddr(localID).Port,
	})
	if err != nil {
		return nil, err
	}
	return newHole(hole, localID, isPinger, conn, opts...), nil
}

// NewHoleWithConn creates a hole with an injected PacketConn (for testing).
func NewHoleWithConn(hole nat.Hole, localID *astral.Identity, isPinger bool, conn net.PacketConn, opts ...HoleOption) *Hole {
	return newHole(hole, localID, isPinger, conn, opts...)
}

func newHole(hole nat.Hole, localID *astral.Identity, isPinger bool, conn net.PacketConn, opts ...HoleOption) *Hole {
	h := &Hole{
		Hole:          hole,
		localIdentity: localID,
		isPinger:      isPinger,
		conn:          conn,
		pingEvents:    make(chan pingEvent, 64),
		wakeCh:        make(chan struct{}, 1),
		lockedCh:      make(chan struct{}),
		pings:         sig.Map[astral.Nonce, int64]{},

		pingInterval:  defaultPingInterval,
		noPingTimeout: defaultNoPingTimeout,
		pingLifespan:  defaultPingLifespan,
		lockTimeout:   defaultLockTimeout,
		maxPingFails:  defaultMaxPingFails,
	}

	for _, opt := range opts {
		opt(h)
	}

	h.state.Store(int32(StateIdle))
	h.lastPing.Store(time.Now().UnixNano())

	return h
}

// isExpectedAddr checks if the given UDP address matches the expected remote address.
func (h *Hole) isExpectedAddr(addr *net.UDPAddr) bool {
	expected := h.GetRemoteAddr(h.localIdentity)
	return addr.IP.Equal(expected.IP) && addr.Port == expected.Port
}

// StartKeepAlive starts the receiver and run goroutines for keepalive management.
func (h *Hole) StartKeepAlive(ctx context.Context) error {
	go h.receiver()
	go h.run(ctx)
	return nil
}

// receiver listens for UDP packets, validates addresses, parses frames, and queues events.
func (h *Hole) receiver() {
	buf := make([]byte, 512)

	for {
		n, addr, err := h.conn.ReadFrom(buf)
		if err != nil {
			return
		}

		remoteAddr, ok := addr.(*net.UDPAddr)
		if !ok || !h.isExpectedAddr(remoteAddr) {
			continue
		}

		var f pingFrame
		if _, err := f.ReadFrom(bytes.NewReader(buf[:n])); err != nil {
			continue
		}

		h.pingEvents <- pingEvent{nonce: f.Nonce, pong: f.Pong}
		h.wake()
	}
}

// run executes the state machine loop, managing keepalive, locking, and expiration.
func (h *Hole) run(ctx context.Context) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	failCount := 0
	var lastPingSent time.Time
	for {
		h.drainEvents()

		switch HoleState(h.state.Load()) {
		case StateIdle:
			h.expirePings()

			if time.Since(time.Unix(0, h.lastPing.Load())) > h.noPingTimeout {
				h.Expire()
				return
			}

			if h.isPinger && time.Since(lastPingSent) >= h.pingInterval {
				if err := h.sendPing(); err != nil {
					failCount++
					if failCount >= h.maxPingFails {
						h.Expire()
						return
					}
				} else {
					failCount = 0
					lastPingSent = time.Now()
				}
			}
		case StateInLocking:
			h.expirePings()

			if h.pings.Len() == 0 {
				h.finalizeLock()
				return
			}

			if h.lockTimedOut() {
				h.Expire()
				return
			}

		case StateLocked, StateExpired:
			return
		}

		select {
		case <-ctx.Done():
			h.Expire()
			return
		case <-ticker.C:
		case <-h.wakeCh:
		}
	}
}

// drainEvents processes all queued ping events synchronously.
func (h *Hole) drainEvents() {
	for {
		select {
		case ev := <-h.pingEvents:
			h.handlePing(ev)
		default:
			return
		}
	}
}

// handlePing processes a ping event, updating timestamps and responding to pings.
func (h *Hole) handlePing(ev pingEvent) {
	if HoleState(h.state.Load()) >= StateLocked {
		return
	}

	h.lastPing.Store(time.Now().UnixNano())

	if ev.pong {
		if _, ok := h.pings.Delete(ev.nonce); ok {
			h.wake()
		}
	} else {
		_ = h.sendPong(ev.nonce)
	}
}

// sendPing sends a ping frame to the remote peer and tracks it.
func (h *Hole) sendPing() error {
	nonce := astral.NewNonce()
	h.pings.Set(nonce, time.Now().UnixNano())
	f := pingFrame{Nonce: nonce, Pong: false}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return err
	}
	_, err := h.conn.WriteTo(buf.Bytes(), h.GetRemoteAddr(h.localIdentity))
	return err
}

// sendPong sends a pong frame in response to a received ping.
func (h *Hole) sendPong(nonce astral.Nonce) error {
	f := pingFrame{Nonce: nonce, Pong: true}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		return err
	}
	_, err := h.conn.WriteTo(buf.Bytes(), h.GetRemoteAddr(h.localIdentity))
	return err
}

// expirePings removes pings that have exceeded their configured lifespan.
func (h *Hole) expirePings() {
	now := time.Now().UnixNano()
	clone := h.pings.Clone()
	for nonce, ts := range clone {
		if now-ts > h.pingLifespan.Nanoseconds() {
			h.pings.Delete(nonce)
		}
	}
}

// lockTimedOut checks if the locking process has exceeded the timeout.
func (h *Hole) lockTimedOut() bool {
	return time.Since(time.Unix(0, h.lockStart.Load())) > h.lockTimeout
}

// BeginLock initiates locking if the hole is idle, returning success status.
func (h *Hole) BeginLock() bool {
	if !h.state.CompareAndSwap(int32(StateIdle), int32(StateInLocking)) {
		return false
	}

	h.lockStart.Store(time.Now().UnixNano())
	h.wake()
	return true
}

// finalizeLock completes locking by closing the connection and notifying waiters.
func (h *Hole) finalizeLock() bool {
	if !h.state.CompareAndSwap(int32(StateInLocking), int32(StateLocked)) {
		return false
	}

	clone := h.pings.Clone()
	for n := range clone {
		h.pings.Delete(n)
	}

	if h.conn != nil {
		_ = h.conn.Close()
	}

	select {
	case <-h.lockedCh:
	default:
		close(h.lockedCh)
	}

	return true
}

// Expire transitions to expired state, cleans up resources, and notifies.
func (h *Hole) Expire() {
	h.state.Store(int32(StateExpired))
	_ = h.conn.Close()

	clone := h.pings.Clone()
	for n := range clone {
		h.pings.Delete(n)
	}

	select {
	case <-h.lockedCh:
	default:
		close(h.lockedCh)
	}

	if h.onHoleExpire != nil {
		h.onHoleExpire(h)
	}

	h.wake()
}

// WaitLocked waits for the hole to lock or the context to cancel.
func (h *Hole) WaitLocked(ctx context.Context) error {
	if HoleState(h.state.Load()) == StateLocked {
		return nil
	}

	h.wake()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.lockedCh:
		if HoleState(h.state.Load()) == StateLocked {
			return nil
		}
		return natmod.ErrHoleCantLock
	}
}

// State returns the current state of the hole.
func (h *Hole) State() HoleState {
	return HoleState(h.state.Load())
}

// IsIdle returns true if the hole is in idle state.
func (h *Hole) IsIdle() bool {
	return HoleState(h.state.Load()) == StateIdle
}

// IsExpired returns true if the hole is expired.
func (h *Hole) IsExpired() bool {
	return HoleState(h.state.Load()) == StateExpired
}

// IsLocked returns true if the hole is locked.
func (h *Hole) IsLocked() bool {
	return HoleState(h.state.Load()) == StateLocked
}

// LastPing returns the timestamp of the last received ping/pong.
func (h *Hole) LastPing() time.Time {
	return time.Unix(0, h.lastPing.Load())
}

// LockTimeout returns the maximum duration for locking handshake.
func (h *Hole) LockTimeout() time.Duration {
	return h.lockTimeout
}

// wake sends a non-blocking wake signal to the run loop.
func (h *Hole) wake() {
	select {
	case h.wakeCh <- struct{}{}:
	default:
	}
}

// Options
type OnHoleExpire func(h *Hole)

type HoleOption func(*Hole)

func WithPingInterval(d time.Duration) HoleOption {
	return func(h *Hole) { h.pingInterval = d }
}

func WithNoPingTimeout(d time.Duration) HoleOption {
	return func(h *Hole) { h.noPingTimeout = d }
}

func WithPingLifespan(d time.Duration) HoleOption {
	return func(h *Hole) { h.pingLifespan = d }
}

func WithLockTimeout(d time.Duration) HoleOption {
	return func(h *Hole) { h.lockTimeout = d }
}

func WithMaxPingFails(n int) HoleOption {
	return func(h *Hole) { h.maxPingFails = n }
}

func WithOnHoleExpire(f OnHoleExpire) HoleOption {
	return func(h *Hole) { h.onHoleExpire = f }
}
