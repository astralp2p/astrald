package tor

import (
	"context"
	"net"

	"golang.org/x/net/proxy"
)

// unixProxyAddr stands in for a unix socket proxy's address inside the SOCKS5 dialer.
//
// why: x/net/proxy parses the proxy address as host:port, so a unix socket is reached through
// the forward dialer, and the dialer is given a placeholder it can parse.
const unixProxyAddr = "unix-socket:0"

// newSOCKSDialer returns a SOCKS5 dialer through the proxy at torProxy, a host:port or a
// "unix:/path" socket, connecting to the proxy through base.
func newSOCKSDialer(torProxy string, base *net.Dialer) (proxy.Dialer, error) {
	network, address := netAddr(torProxy)
	if network != "unix" {
		return proxy.SOCKS5(network, address, nil, base)
	}

	return proxy.SOCKS5("tcp", unixProxyAddr, nil, unixDialer{path: address, base: base})
}

// unixDialer connects to the unix socket at path, whatever address it is asked for.
type unixDialer struct {
	path string
	base *net.Dialer
}

func (d unixDialer) Dial(_, _ string) (net.Conn, error) {
	return d.base.Dial("unix", d.path)
}

func (d unixDialer) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	return d.base.DialContext(ctx, "unix", d.path)
}
