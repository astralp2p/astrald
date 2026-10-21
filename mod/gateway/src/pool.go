package gateway

import (
	gatewaymod "github.com/astralp2p/astrald/mod/gateway"
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astral-go/sig"
	"github.com/astralp2p/astrald/mod/exonet"
)

// ConnPool maintains minIdleConns idle socket connections to a gateway.
type ConnPool struct {
	*Module
	ctx       *astral.Context
	socket    gateway.Socket
	gatewayID *astral.Identity

	conns     sig.Set[*idleConn]
	refillCh  chan struct{}
	confirmCh chan struct{} // an idle conn received its first frame
	rejectCh  chan struct{} // an idle conn closed before its first frame
}

func (mod *Module) newConnPool(ctx *astral.Context, gatewayID *astral.Identity, socket gateway.Socket) *ConnPool {
	return &ConnPool{
		ctx:       ctx,
		Module:    mod,
		socket:    socket,
		gatewayID: gatewayID,
		refillCh:  make(chan struct{}, 1),
		confirmCh: make(chan struct{}, 1),
		rejectCh:  make(chan struct{}, 1),
	}
}

// Run keeps the pool at minIdleConns connections, retrying on failure with exponential backoff.
// A failure is a failed dial or an idle conn the gateway closes before its first frame.
// Returns gateway.ErrSocketUnreachable after maxDialFails consecutive failures, or the context error on shutdown.
func (p *ConnPool) Run() error {
	retry, err := sig.NewRetry(time.Second, 30*time.Second, 2)
	if err != nil {
		return err
	}
	p.notify()

	backoff := func() error {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case count := <-retry.Retry():
			if count >= maxDialFails {
				return gatewaymod.ErrSocketUnreachable
			}
		}
		return nil
	}

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-p.confirmCh:
			retry.Reset()
		case <-p.rejectCh:
			if err := backoff(); err != nil {
				return err
			}
			// why: conns rejected during one backoff count as one failure; the refill redials all of them.
			select {
			case <-p.rejectCh:
			default:
			}
			p.notify()
		case <-p.refillCh:
			// why: the deficit is counted once, so a conn rejected during the refill waits for the backoff.
			for n := minIdleConns - p.idleCount(); n > 0; n-- {
				conn, err := p.dialSocket()
				if err != nil {
					if err := backoff(); err != nil {
						return err
					}
					p.notify()
					break
				}
				p.addIdleConn(conn)
			}
		}
	}
}

func (p *ConnPool) dialSocket() (exonet.Conn, error) {
	p.log.Logv(2, "acquiring socket connection to %v through %v", p.gatewayID, p.socket.Endpoint)
	conn, err := p.Exonet.Dial(p.ctx, p.socket.Endpoint)
	if err != nil {
		return nil, err
	}

	// why: handleInbound reads the nonce as a framed object, the encoding Dial sends.
	if err := channel.New(conn).Send(&p.socket.Nonce); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// idleCount returns the number of conns not yet handed off or closed.
func (p *ConnPool) idleCount() int {
	return len(p.conns.Select(func(a *idleConn) bool {
		select {
		case <-a.Ready():
			return false
		case <-a.Done():
			return false
		default:
			return true
		}
	}))
}

func (p *ConnPool) addIdleConn(conn exonet.Conn) {
	idleConn := newIdleConn(conn, roleClient, p.gatewayID, p.log)
	p.conns.Add(idleConn)

	go func() {
		go idleConn.eventLoop(p.ctx)

		select {
		case <-idleConn.Confirmed():
		case <-idleConn.Done():
		}

		select {
		case <-idleConn.Confirmed():
			signal(p.confirmCh)
		default:
			p.conns.Remove(idleConn)
			signal(p.rejectCh)
			return
		}

		defer func() {
			p.conns.Remove(idleConn)
			p.notify()
		}()

		select {
		case <-idleConn.Ready():
			gwConn := newGatewayConn(idleConn, gateway.NewEndpoint(p.node.Identity(), p.node.Identity()), gateway.NewEndpoint(p.gatewayID, p.node.Identity()))
			err := p.Nodes.EstablishInboundLink(p.ctx, gwConn)
			if err != nil {
				idleConn.Close()
			}

		case <-idleConn.Done():
			return
		}

		<-idleConn.Done()
	}()
}

func (p *ConnPool) notify() {
	signal(p.refillCh)
}

// signal sends to a buffered channel without blocking; a pending signal absorbs the send.
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
