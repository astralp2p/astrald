package gateway

import (
	gatewaymod "github.com/cryptopunkscc/astrald/mod/gateway"
	"sync"

	"github.com/cryptopunkscc/astral-go/api/gateway"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/exonet"
)

// registeredNode represents a node registered as reachable through the gateway.
// Only one registration per identity is allowed.
type registeredNode struct {
	Identity  *astral.Identity
	Nonce     astral.Nonce
	idleConns sig.Set[*idleConn]

	mu         sync.RWMutex
	visibility gateway.Visibility
}

func (b *registeredNode) GetVisibility() gateway.Visibility {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.visibility
}

func (b *registeredNode) SetVisibility(v gateway.Visibility) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.visibility = v
}

func (b *registeredNode) registerConn(conn exonet.Conn, l *log.Logger) *idleConn {
	bc := newIdleConn(conn, roleGateway, b.Identity, l)
	b.idleConns.Add(bc)
	go func() { <-bc.Done(); b.idleConns.Remove(bc) }()
	return bc
}

// claimConn atomically reserves an idle idleConn for a connector.
// Uses handoffOnce as the claim token: the first caller wins.
func (b *registeredNode) claimConn() (*idleConn, bool) {
	for _, c := range b.idleConns.Clone() {
		claimed := false
		c.handoffOnce.Do(func() {
			claimed = true
			close(c.handoffCh)
		})
		if claimed {
			return c, true
		}
	}
	return nil, false
}

func (b *registeredNode) Close() error {
	for _, c := range b.idleConns.Clone() {
		c.Close()
	}
	return nil
}

func (mod *Module) register(ctx *astral.Context, identity *astral.Identity, visibility gateway.Visibility, network string) (gateway.Socket, error) {
	if !mod.canGateway(identity) {
		return gateway.Socket{}, gatewaymod.ErrGatewayDenied
	}

	endpoint, err := mod.getGatewayEndpoint(network)
	if err != nil {
		return gateway.Socket{}, err
	}

	// If already registered, just update visibility and reuse existing connections.
	if existing, ok := mod.registeredNodes.Get(identity.String()); ok {
		existing.SetVisibility(visibility)
		return gateway.Socket{
			Nonce:    existing.Nonce,
			Endpoint: endpoint,
		}, nil
	}

	node := &registeredNode{
		Identity: identity,
		Nonce:    astral.NewNonce(),
	}
	node.SetVisibility(visibility)

	mod.registeredNodes.Set(identity.String(), node)

	return gateway.Socket{
		Nonce:    node.Nonce,
		Endpoint: endpoint,
	}, nil
}

func (mod *Module) unregister(identity *astral.Identity) error {
	node, ok := mod.registeredNodes.Delete(identity.String())
	if !ok {
		return gatewaymod.ErrNodeNotRegistered
	}

	err := node.Close()
	if err != nil {
		mod.log.Error("failed to close registered node: %v", err)
	}

	targetID := identity.String()
	for _, c := range mod.connectors.Clone() {
		if c.Target.String() == targetID {
			mod.connectors.Remove(c)
			err = c.Close()
			if err != nil {
				mod.log.Error("failed to close connector: %v", err)
			}
		}
	}

	return nil
}
