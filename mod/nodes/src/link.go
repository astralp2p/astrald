package nodes

import (
	"errors"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"io"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/nodes/frames"
)

type Ping struct {
	nonce  astral.Nonce
	sentAt time.Time
	pong   chan struct{}
}

// Link is a live mux channel to a peer with its own ping/keepalive loop and pressure tracking; closes once on first error.
type Link struct {
	*channel.Channel
	mux         *Mux
	id          astral.Nonce
	createdAt   time.Time
	conn        astral.Conn
	err         sig.Value[error]
	done        chan struct{}
	pingMu      sync.Mutex
	ping        *Ping
	throughput  atomic.Uint64
	outbound    bool
	prunable    atomic.Bool   // a strategy link no caller asked for as new; a link that outranks it closes it
	idlePing    time.Duration // N: probe after this long without a received frame
	pingTimeout time.Duration // T: close when a probe gets no pong within this
	lastRecv    atomic.Int64  // unix nanos of the last received frame that is not a peer's probe
	wakeCh      chan struct{}
	pressure    LinkPressureDetector
}

func (s *Link) GetMux() *Mux {
	return s.mux
}

func (s *Link) LocalIdentity() *astral.Identity {
	return s.conn.LocalIdentity()
}

func (s *Link) RemoteIdentity() *astral.Identity {
	return s.conn.RemoteIdentity()
}

// CloseWithError records err as the link's cause of death; only the first call's error is kept.
func (s *Link) CloseWithError(err error) error {
	if err == nil {
		err = errors.New("link closed")
	}
	s.err.Swap(nil, err)
	return s.Channel.Close()
}

func (s *Link) Err() error {
	return s.err.Get()
}

func (s *Link) Done() <-chan struct{} {
	return s.done
}

func (s *Link) readLoop() {
	var err error
	defer func() {
		s.err.Swap(nil, err)
		s.Channel.Close()
		close(s.done)
	}()

	for {
		obj, recvErr := s.Receive()
		if recvErr != nil {
			err = recvErr
			return
		}
		s.stampRecv(obj)
		if handleErr := s.mux.Handle(obj); handleErr != nil {
			err = handleErr
			return
		}
	}
}

func (s *Link) Close() error {
	return s.CloseWithError(nil)
}

func (s *Link) Network() string {
	return networkOf(s.conn)
}

func (s *Link) LocalEndpoint() exonet.Endpoint {
	if c, ok := s.conn.(exonetmod.Conn); ok {
		return c.LocalEndpoint()
	}
	return nil
}

func (s *Link) RemoteEndpoint() exonet.Endpoint {
	if c, ok := s.conn.(exonetmod.Conn); ok {
		return c.RemoteEndpoint()
	}

	return nil
}

func (s *Link) Outbound() bool {
	return s.outbound
}

func (s *Link) PressureHigh() bool {
	return s.pressure != nil && s.pressure.IsHigh()
}

func (s *Link) Throughput() uint64 {
	return s.throughput.Load()
}

func (s *Link) onBytes(n int) {
	s.throughput.Add(uint64(n))
	if s.pressure != nil {
		s.pressure.OnBytes(n, time.Now())
	}
}

func (s *Link) RouteQuery(ctx *astral.Context, q *astral.InFlightQuery, w io.WriteCloser) (io.WriteCloser, error) {
	return s.mux.RouteQuery(ctx, q, w)
}

// Ping sends one ping and blocks for the pong. A call while another ping waits
// for its pong joins that ping and shares its outcome and deadline.
// The returned duration is measured from just before the frame is written, so it includes the local send.
// The ping timeout covers the write too: a peer that stopped reading fails the ping within pingTimeout.
// why join: the keepalive closes a link on any Ping error, so a second caller
// must never make the keepalive's own ping fail.
func (s *Link) Ping() (time.Duration, error) {
	p, sent := s.beginPing()
	if sent != nil {
		defer s.clearPing(p)
	}

	timeout := time.NewTimer(time.Until(p.sentAt.Add(s.pingTimeout)))
	defer timeout.Stop()

	for {
		select {
		case err := <-sent:
			if err != nil {
				return -1, err
			}
			sent = nil
		case <-p.pong:
			return time.Since(p.sentAt), nil
		case <-timeout.C:
			return -1, errors.New("ping timeout")
		case <-s.done:
			return -1, s.err.Get()
		}
	}
}

// beginPing returns the ping in flight with a nil channel, or publishes and sends a new ping and returns the channel its send result arrives on.
func (s *Link) beginPing() (*Ping, <-chan error) {
	s.pingMu.Lock()
	defer s.pingMu.Unlock()

	if s.ping != nil {
		return s.ping, nil
	}

	// why: pong() reaches p through s.ping under pingMu, so the stamp must sit
	// inside this critical section to be visible and race-free. Stamping after
	// the send loses the race outright: a pong handled first reads the zero time
	// and reports an RTT of about 292 years.
	p := &Ping{
		nonce:  astral.NewNonce(),
		pong:   make(chan struct{}),
		sentAt: time.Now(),
	}
	s.ping = p

	// why a goroutine: the link conn has no write deadline, and Send blocks on
	// a peer that stopped reading. The timer runs from before the write.
	// note: a write still blocked at the timeout returns once pingLoop closes
	// the link, so the goroutine ends with it.
	sent := make(chan error, 1)
	go func() { sent <- s.Send(&frames.Ping{Nonce: p.nonce}) }()
	return p, sent
}

// clearPing drops p as the in-flight ping, unless a pong or a newer ping already replaced it.
func (s *Link) clearPing(p *Ping) {
	s.pingMu.Lock()
	defer s.pingMu.Unlock()
	if s.ping == p {
		s.ping = nil
	}
}

func (s *Link) pong(nonce astral.Nonce) (time.Duration, error) {
	s.pingMu.Lock()
	p := s.ping
	if p == nil || p.nonce != nonce {
		s.pingMu.Unlock()
		return -1, errors.New("invalid pong nonce")
	}
	s.ping = nil
	s.pingMu.Unlock()
	d := time.Since(p.sentAt)
	close(p.pong)
	if s.pressure != nil {
		s.pressure.OnRTT(d, time.Now())
	}
	return d, nil
}

// Wake triggers a ping on the next loop iteration.
func (s *Link) Wake() {
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
}

func (s *Link) pingLoop() {
	t := time.NewTimer(time.Duration(rand.Int63n(int64(pingJitter))))
	defer t.Stop()

	select {
	case <-s.done:
		return
	case <-t.C:
	}

	for {
		if !s.waitProbe(t) {
			return
		}

		if s.Err() != nil {
			return
		}

		if _, err := s.Ping(); err != nil {
			s.CloseWithError(err)
			return
		}
	}
}

func newLink(mod *Module, conn astral.Conn, id astral.Nonce, outbound bool) *Link {
	ch := channel.New(conn, channel.WithLockedWrites())
	keepalive := keepaliveOf(networkOf(conn), conn)
	link := &Link{
		Channel:     ch,
		id:          id,
		conn:        conn,
		createdAt:   time.Now(),
		outbound:    outbound,
		idlePing:    keepalive.Idle,
		pingTimeout: keepalive.Timeout,
		wakeCh:      make(chan struct{}, 1),
		done:        make(chan struct{}),
	}
	link.lastRecv.Store(link.createdAt.UnixNano())

	link.mux = newMux(
		mod,
		ch,
		link.LocalIdentity(),
		link.RemoteIdentity(),
		link.onBytes,
		link.pong,
	)

	go link.readLoop()
	go link.pingLoop()

	return link
}
