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

	// why: unbuffered, so a send succeeds only while Dial is still waiting
	var connCh = make(chan net.Conn)
	var errCh = make(chan error)

	// Attempt a connection in the background
	go func() {
		c, err := mod.proxy.DialContext(ctx, "tcp", e.Address())
		if err != nil {
			select {
			case errCh <- err:
			case <-ctx.Done():
			}
			return
		}

		// note: ctx is cancelled once Dial returns, so the goroutine owns and
		// closes a late conn; Dial never returns (nil, nil).
		select {
		case connCh <- c:
		case <-ctx.Done():
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
