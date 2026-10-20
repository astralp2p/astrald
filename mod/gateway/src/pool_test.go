package gateway

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
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
