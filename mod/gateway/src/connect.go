package gateway

import (
	gatewaymod "github.com/astralp2p/astrald/mod/gateway"
	"time"

	"github.com/astralp2p/astral-go/api/gateway"
	"github.com/astralp2p/astral-go/astral"
)

func (mod *Module) reserveConn(caller *astral.Identity, target *astral.Identity, network string) (gateway.Socket, error) {
	if !mod.canGateway(caller) {
		return gateway.Socket{}, gatewaymod.ErrGatewayDenied
	}

	endpoint, err := mod.getGatewayEndpoint(network)
	if err != nil {
		return gateway.Socket{}, err
	}

	node, ok := mod.registeredNodeByIdentity(target)
	if !ok {
		return gateway.Socket{}, gatewaymod.ErrTargetNotReachable
	}

	reserved, ok := node.claimConn()
	if !ok {
		return gateway.Socket{}, gatewaymod.ErrTargetNotReachable
	}

	c := &connector{
		Identity: caller,
		Nonce:    astral.NewNonce(),
		Target:   target,
		reserved: reserved,
	}

	mod.connectors.Add(c)

	go func() {
		t := time.NewTimer(connectTimeout)
		defer t.Stop()
		<-t.C

		idleConn := c.takeIdleConn()
		if idleConn == nil {
			return
		}

		mod.connectors.Remove(c)
		if err := idleConn.Close(); err != nil {
			mod.log.Error("failed to close reserved conn: %v", err)
		}
	}()

	return gateway.Socket{
		Nonce:    c.Nonce,
		Endpoint: endpoint,
	}, nil
}
