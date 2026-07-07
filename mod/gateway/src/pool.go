package gateway

import (
	gatewaymod "github.com/cryptopunkscc/astrald/mod/gateway"
	"time"

	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

// ConnPool maintains minIdleConns idle socket connections to a gateway.
type ConnPool struct {
	*Module
	ctx       *astral.Context
	socket    gateway.Socket
	gatewayID *astral.Identity

	conns    sig.Set[*idleConn]
	refillCh chan struct{}
}

func (mod *Module) newConnPool(ctx *astral.Context, gatewayID *astral.Identity, socket gateway.Socket) *ConnPool {
	return &ConnPool{
		ctx:       ctx,
		Module:    mod,
		socket:    socket,
		gatewayID: gatewayID,
		refillCh:  make(chan struct{}, 1),
	}
}

// Run keeps the pool at minIdleConns connections, retrying on dial failure with exponential backoff.
// Returns gateway.ErrSocketUnreachable after maxDialFails consecutive failures, or the context error on shutdown.
func (p *ConnPool) Run() error {
	retry, err := sig.NewRetry(time.Second, 30*time.Second, 2)
	if err != nil {
		return err
	}
	p.notify()

	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		case <-p.refillCh:
			for p.idleCount() < minIdleConns {
				conn, err := p.dialSocket()
				if err != nil {
					select {
					case <-p.ctx.Done():
						return p.ctx.Err()
					case count := <-retry.Retry():
						if count >= maxDialFails {
							return gatewaymod.ErrSocketUnreachable
						}
					}
					continue
				}
				retry.Reset()
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

	if _, err := p.socket.Nonce.WriteTo(conn); err != nil {
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
		defer func() {
			p.conns.Remove(idleConn)
			p.notify()
		}()

		go idleConn.eventLoop(p.ctx)

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
	select {
	case p.refillCh <- struct{}{}:
	default:
	}
}
