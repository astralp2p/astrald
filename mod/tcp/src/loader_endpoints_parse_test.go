package tcp

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/tcp"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
)

func TestLoadConfigEndpoints(t *testing.T) {
	mod := loadModule(t, "configEndpoints:\n  - tcp:203.0.113.7:1791\n  - 198.51.100.2:80\n")

	var got []string
	for _, e := range mod.configEndpoints {
		got = append(got, e.Address())
	}

	if want := []string{"203.0.113.7:1791", "198.51.100.2:80"}; !slices.Equal(got, want) {
		t.Errorf("configEndpoints = %v; want %v", got, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	mod := loadModule(t, "")

	if got := mod.ListenPort(); got != 1791 {
		t.Errorf("ListenPort() = %d; want 1791", got)
	}
	if got := mod.config.DialTimeout; got != time.Minute {
		t.Errorf("DialTimeout = %v; want %v", got, time.Minute)
	}
	if n := len(mod.configEndpoints); n != 0 {
		t.Errorf("configEndpoints has %d entries; want 0", n)
	}
}

func TestParseNetworks(t *testing.T) {
	mod := &Module{}

	for _, network := range []string{"tcp", "inet"} {
		e, err := mod.Parse(network, "1.2.3.4:5")
		if err != nil {
			t.Errorf("Parse(%q): %v", network, err)
			continue
		}
		if got := e.Address(); got != "1.2.3.4:5" {
			t.Errorf("Parse(%q).Address() = %q; want %q", network, got, "1.2.3.4:5")
		}
	}

	if _, err := mod.Parse("kcp", "1.2.3.4:5"); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Parse(kcp) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
}

func TestUnpackNetworks(t *testing.T) {
	mod := &Module{}

	orig, err := tcp.ParseEndpoint("1.2.3.4:5")
	if err != nil {
		t.Fatalf("tcp.ParseEndpoint: %v", err)
	}

	e, err := mod.Unpack("tcp", orig.Pack())
	if err != nil {
		t.Fatalf("Unpack(tcp): %v", err)
	}
	if got := e.Address(); got != orig.Address() {
		t.Errorf("Unpack(tcp).Address() = %q; want %q", got, orig.Address())
	}

	if _, err := mod.Unpack("udp", orig.Pack()); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Unpack(udp) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
}
