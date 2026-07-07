package tcp

import (
	"context"
	"fmt"
	tcpmod "github.com/cryptopunkscc/astrald/mod/tcp"
	"net"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/api/tcp"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

var _ exonet.EphemeralListener = &Server{}

// Server is a TCP listener that wraps accepted connections and dispatches them through a caller-supplied handler.
// It supports graceful shutdown via Close or context cancellation; both paths are idempotent.
type Server struct {
	*Module
	listenPort astral.Uint16
	listener   net.Listener
	onAccept   exonet.EphemeralHandler
	closed     atomic.Bool
	closedCh   chan struct{}
}

func NewServer(module *Module, listenPort astral.Uint16, onAccept exonet.EphemeralHandler) *Server {
	return &Server{
		Module:     module,
		listenPort: listenPort,
		onAccept:   onAccept,
		closedCh:   make(chan struct{}),
	}
}

func (s *Server) Run(ctx *astral.Context) error {
	addr := fmt.Sprintf(":%d", s.listenPort)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tcp server/run: failed to listen on %v: %w", addr, err)
	}

	s.listener = listener

	endpoint, _ := tcp.ParseEndpoint(listener.Addr().String())

	s.log.Info("started server at %v", endpoint)
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.Done():
		}
	}()

	for {
		rawConn, err := listener.Accept()
		if err != nil {
			if s.closed.Load() || ctx.Err() != nil {
				s.log.Info("stopped server at %v", endpoint)
				return nil
			}

			return fmt.Errorf("tcp server/run: accept failed: %w", err)
		}

		if tc, ok := rawConn.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(30 * time.Second)
		}

		conn := tcpmod.WrapConn(rawConn, false)
		go func() {
			stopListener, err := s.onAccept(ctx, conn)
			if err != nil {
				conn.Close()
				s.log.Errorv(1, "tcp server/onAccept error from %v: %v", conn.RemoteEndpoint(), err)
				return
			}

			if stopListener {
				s.Close()
			}
		}()
	}
}

func (s *Server) Done() <-chan struct{} {
	return s.closedCh
}

// Close stops the server idempotently; concurrent or repeated calls are safe.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	if s.listener != nil {
		return s.listener.Close()
	}

	close(s.closedCh)
	return nil
}

func (mod *Module) startServer(ctx context.Context) {
	listenPort := astral.Uint16(mod.config.ListenPort)
	srv := NewServer(mod, listenPort, mod.acceptAll)
	if err := srv.Run(astral.NewContext(ctx)); err != nil {
		mod.log.Errorv(1, "server error: %v", err)
	}
}
