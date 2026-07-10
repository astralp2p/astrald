package kcp

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/kcp"
)

// CreateEphemeralListener creates an ephemeral KCP endpoint which will start a server that listens on the specified port and adds it to the ephemeralListeners set.
func (mod *Module) CreateEphemeralListener(port astral.Uint16) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if _, ok := mod.ephemeralListeners.Get(port); ok {
		return fmt.Errorf("%w: port %d", kcp.ErrEphemeralListenerExists, port)
	}

	kcpServer := NewServer(mod, port, mod.acceptAll)
	mod.ephemeralListeners.Set(port, kcpServer)

	go func() {
		err := kcpServer.Run(mod.ctx)
		if err != nil {
			mod.log.Error("ephemeral listener error: %v", err)
		}

		mod.ephemeralListeners.Delete(port)
	}()

	return nil
}

func (mod *Module) CloseEphemeralListener(port astral.Uint16) error {
	listener, ok := mod.ephemeralListeners.Get(port)
	if !ok {
		return kcp.ErrEphemeralListenerNotExist
	}

	listener.Close()
	mod.ephemeralListeners.Delete(port)

	return nil
}
