package kcp

import (
	"errors"
	"maps"
	"testing"

	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tcp"
	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	exonetmod "github.com/astralp2p/astrald/mod/exonet"
	kcpmod "github.com/astralp2p/astrald/mod/kcp"
)

const testKCPAddr = "192.0.2.1:1792"

func parseTestEndpoint(t *testing.T) *kcp.Endpoint {
	t.Helper()

	e, err := kcp.ParseEndpoint(testKCPAddr)
	if err != nil {
		t.Fatalf("ParseEndpoint(%q): %v", testKCPAddr, err)
	}
	return e
}

func assertMappings(t *testing.T, mod *Module, want map[astral.String8]astral.Uint16) {
	t.Helper()

	if got := mod.GetEndpointsMappings(); !maps.Equal(got, want) {
		t.Errorf("GetEndpointsMappings() = %v; want %v", got, want)
	}
}

func TestSetEndpointLocalSocket(t *testing.T) {
	mod := &Module{}
	e := parseTestEndpoint(t)

	if err := mod.SetEndpointLocalSocket(*e, 40000, false); err != nil {
		t.Fatalf("first Set(40000, replace=false) = %v; want nil", err)
	}
	assertMappings(t, mod, map[astral.String8]astral.Uint16{testKCPAddr: 40000})

	err := mod.SetEndpointLocalSocket(*e, 40001, false)
	if !errors.Is(err, kcpmod.ErrEndpointLocalSocketExists) {
		t.Errorf("second Set(40001, replace=false) = %v; want %v", err, kcpmod.ErrEndpointLocalSocketExists)
	}
	assertMappings(t, mod, map[astral.String8]astral.Uint16{testKCPAddr: 40000})

	if err := mod.SetEndpointLocalSocket(*e, 40001, true); err != nil {
		t.Errorf("Set(40001, replace=true) = %v; want nil", err)
	}
	assertMappings(t, mod, map[astral.String8]astral.Uint16{testKCPAddr: 40001})
}

func TestRemoveEndpointLocalSocket(t *testing.T) {
	mod := &Module{}
	e := parseTestEndpoint(t)

	if err := mod.SetEndpointLocalSocket(*e, 40000, false); err != nil {
		t.Fatalf("Set: %v", err)
	}

	for i := 1; i <= 2; i++ {
		if err := mod.RemoveEndpointLocalSocket(*e); err != nil {
			t.Errorf("Remove #%d = %v; want nil", i, err)
		}
		assertMappings(t, mod, map[astral.String8]astral.Uint16{})
	}
}

func TestGetEndpointsMappingsReturnsCopy(t *testing.T) {
	mod := &Module{}

	m := mod.GetEndpointsMappings()
	m["x"] = 1

	if _, found := mod.GetEndpointsMappings()["x"]; found {
		t.Error("mutating the returned map changed the module's mappings")
	}
}

func TestParseNetworks(t *testing.T) {
	mod := &Module{}

	e, err := mod.Parse("kcp", testKCPAddr)
	if err != nil {
		t.Fatalf("Parse(kcp): %v", err)
	}
	if got := e.Address(); got != testKCPAddr {
		t.Errorf("Parse(kcp).Address() = %q; want %q", got, testKCPAddr)
	}

	if _, err := mod.Parse("tcp", testKCPAddr); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Parse(tcp) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}
}

func TestUnpackNetworks(t *testing.T) {
	mod := &Module{}
	packed := parseTestEndpoint(t).Pack()

	e, err := mod.Unpack("kcp", packed)
	if err != nil {
		t.Fatalf("Unpack(kcp): %v", err)
	}
	if got := e.Address(); got != testKCPAddr {
		t.Errorf("Unpack(kcp).Address() = %q; want %q", got, testKCPAddr)
	}

	if _, err := mod.Unpack("udp", packed); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Unpack(udp) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}

	if _, err := mod.Unpack("kcp", []byte{1}); err == nil {
		t.Error("Unpack(kcp, 1 byte) returned nil error; want an error")
	}
}

func TestDialGating(t *testing.T) {
	ctx := astral.NewContext(nil)
	mod := &Module{settings: Settings{Dial: &tree.Value[*astral.Bool]{}}}

	tcpEndpoint, err := tcp.ParseEndpoint("192.0.2.1:1791")
	if err != nil {
		t.Fatalf("tcp.ParseEndpoint: %v", err)
	}
	if _, err := mod.Dial(ctx, tcpEndpoint); !errors.Is(err, exonetmod.ErrUnsupportedNetwork) {
		t.Errorf("Dial(tcp endpoint) = %v; want %v", err, exonetmod.ErrUnsupportedNetwork)
	}

	off := astral.Bool(false)
	if err := mod.settings.Dial.Set(ctx, &off); err != nil {
		t.Fatalf("settings.Dial.Set(false): %v", err)
	}

	// note: the gate returns before a UDP socket is bound, so no network is touched.
	if _, err := mod.Dial(ctx, parseTestEndpoint(t)); !errors.Is(err, exonetmod.ErrDisabledNetwork) {
		t.Errorf("Dial(kcp endpoint) with dial off = %v; want %v", err, exonetmod.ErrDisabledNetwork)
	}
}
