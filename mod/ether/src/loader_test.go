package ether

import (
	"fmt"
	"net"
	"testing"

	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core/assets"
	"gopkg.in/yaml.v2"
)

// yamlAssets serves a single ether.yaml from memory.
//
// why: the embedded nil interface satisfies assets.Assets without implementing
// it. Any method other than LoadYAML panics, which is the assertion that Load
// reads its config file and nothing else.
type yamlAssets struct {
	assets.Assets
	yaml string
}

func (a yamlAssets) LoadYAML(_ string, out any) error {
	return yaml.Unmarshal([]byte(a.yaml), out)
}

// TestLoadListen pins that Load binds the discovery socket only when listen is
// on, and that a missing listen key leaves it on.
func TestLoadListen(t *testing.T) {
	port := reserveUDPPort(t)

	for _, listen := range []bool{true, false} {
		config := fmt.Sprintf("udp_port: %v\n", port)
		if !listen {
			config += "listen: false\n"
		}

		loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, &log.Logger{})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}

		mod := loaded.(*Module)
		if bound := mod.socket != nil; bound != listen {
			t.Fatalf("listen %v: socket bound = %v", listen, bound)
		}
		if mod.socket != nil {
			mod.socket.Close()
		}
	}
}

// reserveUDPPort returns a UDP port that was free a moment ago.
func reserveUDPPort(t *testing.T) int {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer conn.Close()

	return conn.LocalAddr().(*net.UDPAddr).Port
}

// TestLoadSendsWithTheLinkTTL pins that the bound socket sends every datagram with IP TTL 255.
func TestLoadSendsWithTheLinkTTL(t *testing.T) {
	config := fmt.Sprintf("udp_port: %v\n", reserveUDPPort(t))

	loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, &log.Logger{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	mod := loaded.(*Module)
	if mod.socket == nil {
		t.Fatal("socket not bound")
	}
	defer mod.socket.Close()

	ttl, err := mod.socket.TTL()
	if err != nil {
		t.Skipf("the platform cannot read back the TTL: %v", err)
	}
	if ttl != 255 {
		t.Errorf("socket TTL = %v; want 255", ttl)
	}
}

// TestCloseReleasesTheSocket: after Close, the next Load in the same process
// binds the same port.
func TestCloseReleasesTheSocket(t *testing.T) {
	config := fmt.Sprintf("udp_port: %v\n", reserveUDPPort(t))
	for i := 1; i <= 2; i++ {
		loaded, err := Loader{}.Load(nil, yamlAssets{yaml: config}, &log.Logger{})
		if err != nil {
			t.Fatalf("Load %v: %v", i, err)
		}
		mod := loaded.(*Module)
		if mod.socket == nil {
			t.Fatalf("Load %v: socket not bound", i)
		}
		if err := mod.Close(); err != nil {
			t.Fatalf("Close %v: %v", i, err)
		}
	}
}
