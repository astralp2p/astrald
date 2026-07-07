package tor

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/exonet"
	"github.com/cryptopunkscc/astrald/mod/tor"
	"net"
)

var _ exonet.Dialer = &Module{}

// Dial tries to establish a Tor connection to the provided address
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (conn exonet.Conn, err error) {
	if dial := mod.settings.Dial.Get(); dial != nil && !*dial {
		return nil, exonet.ErrDisabledNetwork
	}

	endpoint, err = mod.Unpack(endpoint.Network(), endpoint.Pack())
	if err != nil {
		return nil, err
	}

	var e = endpoint.(*tor.Endpoint)

	ctx, cancel := ctx.WithTimeout(mod.config.DialTimeout)
	defer cancel()

	var connCh = make(chan net.Conn, 1)
	var errCh = make(chan error, 1)

	// Attempt a connection in the background
	go func() {
		defer close(connCh)
		defer close(errCh)

		c, err := mod.proxy.DialContext(ctx, "tcp", e.Address())
		if err != nil {
			errCh <- err
			return
		}

		// Return the connection if we're still waiting for it, close it otherwise
		select {
		case connCh <- c:
		default:
			c.Close()
		}
	}()

	// Wait for the first result
	select {
	case c := <-connCh:
		return newConn(c, e, true), nil
	case err = <-errCh:
		return nil, err
	case <-ctx.Done():
		err = ctx.Err()
		return nil, err
	}
}
