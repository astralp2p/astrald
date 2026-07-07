package gateway

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/channel"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

var ErrConnClosed = errors.New("conn closed")

type connRole uint8

const (
	roleClient connRole = iota
	roleGateway
)

// note: maybe can be part of exonet
type deadliner interface {
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
}

type idleConn struct {
	exonet.Conn
	role         connRole
	log          *log.Logger
	withIdentity *astral.Identity // identity of peer we are connected to

	closed atomic.Bool

	handoffCh   chan struct{} // gateway only
	handoffOnce sync.Once

	readyCh   chan struct{}
	doneCh    chan struct{}
	readyOnce sync.Once
	doneOnce  sync.Once
}

func newIdleConn(conn exonet.Conn, role connRole, identity *astral.Identity, l *log.Logger) *idleConn {
	c := &idleConn{
		Conn:         conn,
		role:         role,
		log:          l,
		withIdentity: identity,
		readyCh:      make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
	if role == roleGateway {
		c.handoffCh = make(chan struct{})
	}
	l.Logv(2, "idle conn created with %v remote %v", identity, conn.RemoteEndpoint())
	return c
}

func (conn *idleConn) Ready() <-chan struct{} { return conn.readyCh }
func (conn *idleConn) Done() <-chan struct{}  { return conn.doneCh }

func (conn *idleConn) markReady() {
	conn.readyOnce.Do(func() { close(conn.readyCh) })
}

func (conn *idleConn) Close() error {
	if conn.closed.Swap(true) {
		return nil
	}
	err := conn.Conn.Close()
	conn.doneOnce.Do(func() { close(conn.doneCh) })
	return err
}

func (conn *idleConn) activate(ctx context.Context) error {
	if conn.role != roleGateway {
		return errors.New("activate called on non-gateway conn")
	}
	conn.handoffOnce.Do(func() { close(conn.handoffCh) })

	select {
	case <-conn.readyCh:
		return nil
	case <-conn.doneCh:
		return ErrConnClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (conn *idleConn) setReadDeadline(t time.Time) {
	if dl, ok := conn.Conn.(deadliner); ok {
		dl.SetReadDeadline(t)
	}
}

func (conn *idleConn) setWriteDeadline(t time.Time) {
	if dl, ok := conn.Conn.(deadliner); ok {
		dl.SetWriteDeadline(t)
	}
}

func (conn *idleConn) eventLoop(ctx context.Context) {
	ch := channel.New(conn.Conn)

	lastActivity := time.Now()
	lastPing := time.Time{}
	var handoffDone bool
	var handoffSuccess bool

	defer func() {
		if !handoffSuccess {
			conn.Close()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		now := time.Now()

		if conn.role == roleGateway && !handoffDone {
			select {
			case <-conn.handoffCh:
				conn.setWriteDeadline(now.Add(writeTimeout))
				if err := ch.Send(&Handoff{}); err != nil {
					return
				}
				handoffDone = true
				lastActivity = now
				lastPing = now
			default:
			}
		}

		if now.Sub(lastActivity) >= pingInterval && now.Sub(lastPing) >= pingInterval {
			conn.setWriteDeadline(now.Add(writeTimeout))
			if err := ch.Send(&Ping{}); err != nil {
				return
			}
			lastPing = now
		}

		readWait := pingTimeout
		if conn.role == roleGateway && !handoffDone {
			readWait = handoffPollInterval
		}

		conn.setReadDeadline(now.Add(readWait))

		obj, err := ch.Receive()
		if err != nil {
			if isTimeout(err) {
				if time.Since(lastActivity) >= pingTimeout {
					conn.log.Logv(2, "closing idle conn with %v idle for %v", conn.withIdentity, time.Since(lastActivity).Round(time.Second).String())
					return
				}
				continue
			}

			return
		}

		lastActivity = time.Now()

		switch m := obj.(type) {
		case *Ping:
			if !m.Pong {
				conn.setWriteDeadline(time.Now().Add(writeTimeout))
				if err := ch.Send(&Ping{Pong: true}); err != nil {
					return
				}
			}

		case *Handoff:
			conn.setWriteDeadline(time.Now().Add(writeTimeout))
			if err := ch.Send(&HandoffAck{}); err != nil {
				return
			}
			handoffSuccess = true
			conn.setReadDeadline(time.Time{})
			conn.setWriteDeadline(time.Time{})
			conn.markReady()
			return

		case *HandoffAck:
			handoffSuccess = true
			conn.setReadDeadline(time.Time{})
			conn.setWriteDeadline(time.Time{})
			conn.markReady()
			return

		default:
			return
		}
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}
