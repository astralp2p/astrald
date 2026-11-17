package tor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/astralp2p/astrald/lib/paths"
)

func TestNetAddr(t *testing.T) {
	for _, c := range []struct{ in, network, address string }{
		{"127.0.0.1:9050", "tcp", "127.0.0.1:9050"},
		{"tcp:127.0.0.1:9051", "tcp", "127.0.0.1:9051"},
		{"unix:/run/tor/control", "unix", "/run/tor/control"},
		{"[::1]:9050", "tcp", "[::1]:9050"},
	} {
		network, address := netAddr(c.in)
		if network != c.network || address != c.address {
			t.Errorf("netAddr(%q) = %q %q, want %q %q", c.in, network, address, c.network, c.address)
		}
	}
	if got := ipcAddr("127.0.0.1:9050"); got != "tcp:127.0.0.1:9050" {
		t.Errorf("ipcAddr of a bare address = %q", got)
	}
}

// TestResolveAddrsAppliesThePathRule: a unix: address in tor.yaml expands "~/", and a relative
// path is refused with the key it came from; TCP addresses pass unchanged.
func TestResolveAddrsAppliesThePathRule(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}

	c := defaultConfig()
	c.ControlAddr = "unix:~/tor/control"
	if err := c.resolveAddrs(); err != nil {
		t.Fatalf("resolveAddrs: %v", err)
	}
	if want := "unix:" + filepath.Join(home, "tor/control"); c.ControlAddr != want {
		t.Errorf("control_addr = %q, want %q", c.ControlAddr, want)
	}
	if c.TorProxy != "127.0.0.1:9050" || c.OnionTarget != "tcp:127.0.0.1:0" {
		t.Errorf("TCP defaults changed: %q %q", c.TorProxy, c.OnionTarget)
	}

	c = defaultConfig()
	c.TorProxy = "unix:tor/socks"
	err = c.resolveAddrs()
	if !errors.Is(err, paths.ErrNotAbsolute) || !strings.Contains(err.Error(), "tor_proxy") {
		t.Fatalf("a relative tor_proxy path gave %v", err)
	}
}

// TestResolveAddrsRefusesASpaceInTheOnionTarget: ADD_ONION cannot carry such a path to Tor.
func TestResolveAddrsRefusesASpaceInTheOnionTarget(t *testing.T) {
	c := defaultConfig()
	c.OnionTarget = "unix:/data/my node/onion.sock"
	if err := c.resolveAddrs(); err == nil || !strings.Contains(err.Error(), "onion_target") {
		t.Fatalf("a space in onion_target gave %v", err)
	}
}
