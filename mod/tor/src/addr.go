package tor

import (
	"fmt"
	"strings"

	"github.com/astralp2p/astrald/lib/paths"
)

// note: a tor.yaml address takes the IPC form of astral-go lib/ipc, "unix:/path" for a unix
// socket or "tcp:host:port" for TCP. A bare "host:port" is TCP, the form tor.yaml has always used.
const (
	unixPrefix = "unix:"
	tcpPrefix  = "tcp:"
)

// netAddr returns the net package network and address of a tor.yaml address.
func netAddr(addr string) (network, address string) {
	if p, ok := strings.CutPrefix(addr, unixPrefix); ok {
		return "unix", p
	}
	if a, ok := strings.CutPrefix(addr, tcpPrefix); ok {
		return "tcp", a
	}

	return "tcp", addr
}

// ipcAddr returns a tor.yaml address in the "proto:addr" form lib/ipc takes.
func ipcAddr(addr string) string {
	network, address := netAddr(addr)

	return network + ":" + address
}

// resolveAddrs applies the config path rule to the unix: addresses of tor.yaml: "~/" is expanded,
// and a relative path is an error that names its key.
func (c *Config) resolveAddrs() error {
	for _, field := range []struct {
		key  string
		addr *string
	}{
		{"tor_proxy", &c.TorProxy},
		{"control_addr", &c.ControlAddr},
		{"onion_target", &c.OnionTarget},
	} {
		resolved, err := paths.FromConfigAddr(*field.addr)
		if err != nil {
			return fmt.Errorf("tor.yaml %s: %w", field.key, err)
		}
		*field.addr = resolved
	}

	// why: ADD_ONION takes the target unquoted, so a space splits the command (tor#40633)
	if network, p := netAddr(c.OnionTarget); network == "unix" && strings.ContainsAny(p, " \t") {
		return fmt.Errorf("tor.yaml onion_target: %q: Tor cannot take a unix path with a space", p)
	}

	return nil
}
