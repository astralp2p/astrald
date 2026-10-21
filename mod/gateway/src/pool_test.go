package gateway

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	gatewaymod "github.com/astralp2p/astrald/mod/gateway"
)

// pipeConn is one end of an in-memory connection, presented as an exonet.Conn.
type pipeConn struct {
	net.Conn
	outbound bool
}

func (c pipeConn) Outbound() bool                  { return c.outbound }
func (c pipeConn) LocalEndpoint() exonet.Endpoint  { return nil }
func (c pipeConn) RemoteEndpoint() exonet.Endpoint { return nil }

// pipeExonet dials every endpoint to the same prepared connection.
//
// why: the embedded nil interface satisfies exonetmod.Module without implementing
// it. Any method other than Dial panics.
type pipeExonet struct {
	exonetmod.Module
	conn exonetmod.Conn
}

func (e *pipeExonet) Dial(*astral.Context, exonet.Endpoint) (exonetmod.Conn, error) {
	return e.conn, nil
}

// A registered node's pool presents its nonce in the encoding the gateway's
// handleInbound reads, so the gateway adds the connection to that node's idle set.
func TestPoolNonceAddsIdleConnToRegisteredNode(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	actx := astral.NewContext(ctx)

	id := astral.GenerateIdentity()
	logger := log.New(id)
	nonce := astral.Nonce(0x0123456789abcdef)

	gw := &Module{log: logger, ctx: actx}
	node := &registeredNode{Identity: id, Nonce: nonce}
	gw.registeredNodes.Set(id.String(), node)

	clientEnd, gatewayEnd := net.Pipe()
	defer clientEnd.Close()
	defer gatewayEnd.Close()
	// why: a nonce the gateway cannot decode can stall the read rather than fail it.
	gatewayEnd.SetReadDeadline(time.Now().Add(2 * time.Second))

	pool := &ConnPool{
		Module: &Module{
			Deps: Deps{Exonet: &pipeExonet{conn: pipeConn{Conn: clientEnd, outbound: true}}},
			log:  logger,
		},
		ctx:       actx,
		socket:    gateway.Socket{Nonce: nonce},
		gatewayID: id,
	}

	dialErr := make(chan error, 1)
	go func() {
		_, err := pool.dialSocket()
		dialErr <- err
	}()

	if _, err := gw.handleInbound(ctx, pipeConn{Conn: gatewayEnd}); err != nil {
		t.Fatalf("handleInbound rejected the pool's connection: %v", err)
	}
	if err := <-dialErr; err != nil {
		t.Fatalf("dialSocket: %v", err)
	}
	if n := len(node.idleConns.Clone()); n != 1 {
		t.Fatalf("registered node holds %v idle conns, want 1", n)
	}
}

// gatewayExonet dials every endpoint to a fresh in-memory connection whose
// far end the gateway's handleInbound serves.
type gatewayExonet struct {
	exonetmod.Module
	gw    *Module
	dials atomic.Int32
}

func (e *gatewayExonet) Dial(ctx *astral.Context, _ exonet.Endpoint) (exonetmod.Conn, error) {
	e.dials.Add(1)
	clientEnd, gatewayEnd := net.Pipe()
	go e.gw.handleInbound(ctx, pipeConn{Conn: gatewayEnd})
	return pipeConn{Conn: clientEnd, outbound: true}, nil
}

// A gateway that no longer knows the pool's nonce closes every idle conn at
// once. The pool backs off between redials and returns ErrSocketUnreachable
// after maxDialFails, so the node registers again.
func TestPoolReturnsWhenGatewayRejectsNonce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	actx := astral.NewContext(ctx)

	id := astral.GenerateIdentity()
	logger := log.New(id)

	// why: a gateway module with no registered node answers every nonce as unknown.
	ex := &gatewayExonet{gw: &Module{log: logger, ctx: actx}}
	pool := (&Module{Deps: Deps{Exonet: ex}, log: logger}).newConnPool(actx, id, gateway.Socket{Nonce: astral.NewNonce()})

	start := time.Now()
	err := pool.Run()
	elapsed := time.Since(start)

	if !errors.Is(err, gatewaymod.ErrSocketUnreachable) {
		t.Fatalf("Run returned %v after %v and %v dials, want %v", err, elapsed, ex.dials.Load(), gatewaymod.ErrSocketUnreachable)
	}
	t.Logf("Run returned %v after %v and %v dials", err, elapsed.Round(time.Millisecond), ex.dials.Load())
	if n, max := ex.dials.Load(), int32(minIdleConns*maxDialFails); n > max {
		t.Fatalf("pool dialed %v times, want at most %v", n, max)
	}
	// why: the retry delays before the second and third redial rounds are 1s and 2s.
	if elapsed < 3*time.Second {
		t.Fatalf("Run returned after %v, want backoff of at least 3s", elapsed)
	}
}
