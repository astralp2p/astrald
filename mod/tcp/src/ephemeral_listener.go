package tcp

import (
	"fmt"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/exonet"
	"github.com/astralp2p/astrald/mod/tcp"
)

// CreateEphemeralListener starts a TCP server on port and registers it in the ephemeral listener map.
// The server runs in a background goroutine and removes itself from the map when it stops.
// Returns an error if a listener on that port already exists.
func (mod *Module) CreateEphemeralListener(ctx *astral.Context, port astral.Uint16, handler exonet.EphemeralHandler) error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	if _, ok := mod.ephemeralListeners.Get(port); ok {
		return fmt.Errorf("%w: port %v", tcp.ErrEphemeralListenerExists, port)
	}

	srv := NewServer(mod, port, handler)
	mod.ephemeralListeners.Set(port, srv)

	go func() {
		err := srv.Run(ctx)
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
		return tcp.ErrEphemeralListenerNotExist
	}

	listener.Close()
	mod.ephemeralListeners.Delete(port)

	return nil
}
