package gateway

import (
	"context"
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/channel"
	"github.com/astralp2p/astrald/mod/exonet"
)

// handleInbound dispatches an incoming socket connection to either the registered
// node or connector path based on the nonce it presents.
func (mod *Module) handleInbound(_ context.Context, conn exonet.Conn) (stopListener bool, err error) {
	mod.log.Logv(2, "accepting socket connection from %v", conn.RemoteEndpoint())

	ch := channel.New(conn)

	var nonce *astral.Nonce
	// expect node's signature
	err = ch.Switch(channel.Expect(&nonce), channel.PassErrors)
	if err != nil {
		conn.Close()
		return false, err
	}

	if b, ok := mod.registeredNodeByNonce(*nonce); ok {
		mod.log.Infov(2, "added idle conn to registered node %v", b.Identity)
		bc := b.registerConn(conn, mod.log)
		go bc.eventLoop(mod.ctx)
		return stopListener, nil
	}

	c, ok := mod.connectorByNonce(*nonce)
	if !ok {
		mod.log.Errorv(1, "unknown nonce %v from %v", nonce, conn.RemoteEndpoint())
		conn.Close()
		return stopListener, nil
	}

	mod.connectors.Remove(c)

	standby := c.takeIdleConn()
	if standby == nil {
		conn.Close()
		return stopListener, fmt.Errorf("no reserved conn for %v", c.Target)
	}

	if err := standby.activate(mod.ctx); err != nil {
		mod.log.Errorv(1, "activation failed for %v: %v", c.Target, err)
		conn.Close()
		return stopListener, nil
	}

	mod.log.Infov(2, "pipe from %v to %v created", c.Identity, c.Target)
	go pipe(standby, conn)
	return stopListener, nil
}
