package gateway

import (
	"sync"

	"github.com/astralp2p/astral-go/astral"
)

// connector represents a pending connection request from a node that wants
// to reach a registered node through the gateway. Multiple connectors per identity
// are allowed.
type connector struct {
	mu       sync.Mutex
	Identity *astral.Identity
	Nonce    astral.Nonce
	Target   *astral.Identity
	reserved *idleConn
}

// takeIdleConn atomically takes the reserved idleConn, returning nil if
// already taken (connection already established or timed out).
func (c *connector) takeIdleConn() *idleConn {
	c.mu.Lock()
	defer c.mu.Unlock()

	bc := c.reserved
	c.reserved = nil
	return bc
}

func (c *connector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.reserved != nil {
		return c.reserved.Close()
	}

	return nil
}
