package nodes

import (
	"errors"
	exonetmod "github.com/cryptopunkscc/astrald/mod/exonet"
	"io"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/api/exonet"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/nodes/frames"
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
	checks      atomic.Int32
	throughput  atomic.Uint64
	outbound    bool
	pingTimeout time.Duration
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
	if c, ok := s.conn.(exonetmod.Conn); ok {
		if e := c.RemoteEndpoint(); e != nil {
			return e.Network()
		}
		if e := c.LocalEndpoint(); e != nil {
			return e.Network()
		}
	}
	return "unknown"
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

// Ping sends one ping and blocks for the pong. Only one ping may be in flight; concurrent calls error.
func (s *Link) Ping() (time.Duration, error) {
	p := &Ping{
		nonce: astral.NewNonce(),
		pong:  make(chan struct{}),
	}

	s.pingMu.Lock()
	if s.ping != nil {
		s.pingMu.Unlock()
		return -1, errors.New("ping already in flight")
	}
	s.ping = p
	s.pingMu.Unlock()

	err := s.Send(&frames.Ping{
		Nonce: p.nonce,
	})
	if err != nil {
		s.pingMu.Lock()
		if s.ping == p {
			s.ping = nil
		}
		s.pingMu.Unlock()
		return -1, err
	}
	p.sentAt = time.Now()

	select {
	case <-p.pong:
		return time.Since(p.sentAt), nil
	case <-time.After(s.pingTimeout):
		s.pingMu.Lock()
		if s.ping == p {
			s.ping = nil
		}
		s.pingMu.Unlock()
		return -1, errors.New("ping timeout")
	case <-s.done:
		s.pingMu.Lock()
		if s.ping == p {
			s.ping = nil
		}
		s.pingMu.Unlock()
		return -1, s.err.Get()
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
		if s.checks.Load() > 0 {
			t.Reset(activeInterval)
			select {
			case <-s.done:
				return
			case <-t.C:
			}
		} else {
			select {
			case <-s.done:
				return
			case <-s.wakeCh:
			}
		}

		if s.Err() != nil {
			return
		}

		if _, err := s.Ping(); err != nil {
			s.CloseWithError(err)
			return
		}

		if s.checks.Load() > 0 {
			s.checks.Add(-1)
		}
	}
}

func newLink(mod *Module, conn astral.Conn, id astral.Nonce, outbound bool) *Link {
	ch := channel.New(conn, channel.WithLockedWrites())
	link := &Link{
		Channel:     ch,
		id:          id,
		conn:        conn,
		createdAt:   time.Now(),
		outbound:    outbound,
		pingTimeout: defaultPingTimeout,
		wakeCh:      make(chan struct{}, 1),
		done:        make(chan struct{}),
	}

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
