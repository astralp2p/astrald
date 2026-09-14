package kcp

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	kcpmod "github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/exonet"
	kcpgo "github.com/xtaci/kcp-go/v5"
)

var _ exonet.EphemeralListener = &Server{}

// Server implements KCP listening with connection acceptance via kcp.Listener
type Server struct {
	*Module
	listenPort  astral.Uint16
	listener    *kcpgo.Listener
	onAccept    exonet.EphemeralHandler
	closed      atomic.Bool
	closedCh    chan struct{}
	accepted    atomic.Bool
	idleTimeout time.Duration
}

// NewServer builds a KCP server on listenPort. A non-zero idleTimeout closes the
// server when it accepts no connection within that time.
func NewServer(module *Module, listenPort astral.Uint16, onAccept exonet.EphemeralHandler, idleTimeout time.Duration) *Server {
	return &Server{
		Module:      module,
		listenPort:  listenPort,
		onAccept:    onAccept,
		closedCh:    make(chan struct{}),
		idleTimeout: idleTimeout,
	}
}

func (s *Server) Run(ctx *astral.Context) error {
	addr := fmt.Sprintf(":%d", s.listenPort)
	kcpListener, err := kcpgo.ListenWithOptions(addr, nil, 0, 0)
	if err != nil {
		return fmt.Errorf("kcp server/run: failed to listen on %v: %w", addr, err)
	}

	s.listener = kcpListener

	localEndpoint, err := kcpmod.ParseEndpoint(kcpListener.Addr().String())
	if err != nil {
		return fmt.Errorf(`kcp server/run: failed to parse local endpoint %v: %w`,
			kcpListener.Addr(), err)
	}

	s.log.Info("started server at %v", kcpListener.Addr())
	go func() {
		select {
		case <-ctx.Done():
			s.Close()
		case <-s.Done():
		}
	}()

	// why: a NAT traversal that fails after the peer created this listener never
	// connects to it, and nothing else reclaims the port.
	// note: a listener that accepted a connection is never reaped here. Close
	// closes the UDP socket its accepted sessions read and write through, so
	// reaping it would kill every live link on this port.
	if s.idleTimeout > 0 {
		idle := time.AfterFunc(s.idleTimeout, func() {
			if s.accepted.Load() {
				return
			}

			s.log.Logv(1, "closing unused ephemeral listener on port %v after %v",
				s.listenPort, s.idleTimeout)
			s.Close()
		})
		defer idle.Stop()
	}

	for {
		sess, err := kcpListener.AcceptKCP()
		if err != nil {
			if s.closed.Load() || ctx.Err() != nil {
				s.log.Info("stopped server at %v", kcpListener.Addr())
				return nil
			}

			return fmt.Errorf("kcp server/run: accept failed: %w", err)
		}

		s.accepted.Store(true)

		remoteEndpoint, _ := kcpmod.ParseEndpoint(sess.RemoteAddr().String())
		s.log.Info("accepted connection from %v", remoteEndpoint)

		conn := WrapKCPConn(sess, localEndpoint, remoteEndpoint, false, 1*time.Minute)
		go func() {
			shouldClose, err := s.onAccept(ctx, conn)
			if err != nil {
				s.log.Errorv(1, "kcp server/onAccept error from %v: %v", conn.RemoteEndpoint(), err)
				return
			}

			if shouldClose {
				s.Close()
			}
		}()
	}
}

func (s *Server) Done() <-chan struct{} {
	return s.closedCh
}

// Close stops the server idempotently; the first call closes the underlying
// KCP listener (or the done channel if no listener was started yet), and
// subsequent calls are no-ops.
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
	// why: the node's own listener is not ephemeral, so it carries no idle timeout.
	srv := NewServer(mod, listenPort, mod.acceptAll, 0)
	if err := srv.Run(astral.NewContext(ctx)); err != nil {
		mod.log.Errorv(1, "server error: %v", err)
	}
}
