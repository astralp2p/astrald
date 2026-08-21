package tor

import (
	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/tor"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	"net"
)

var _ exonetmod.Dialer = &Module{}

// Dial tries to establish a Tor connection to the provided address
func (mod *Module) Dial(ctx *astral.Context, endpoint exonet.Endpoint) (conn exonetmod.Conn, err error) {
	if dial := mod.settings.Dial.Get(); dial != nil && !*dial {
		return nil, exonetmod.ErrDisabledNetwork
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
