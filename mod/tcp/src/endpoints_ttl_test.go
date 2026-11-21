package tcp

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/tcp"
	ipmod "github.com/astralp2p/astrald/mod/ip"
)

func mustIP(t *testing.T, s string) ip.IP {
	t.Helper()
	a, err := ip.ParseIP(s)
	if err != nil {
		t.Fatalf("ParseIP(%v): %v", s, err)
	}
	return a
}

// TestEndpointsTTLByAddressClass (criterion 1): a private, a CGNAT and a
// public interface address get LocalEndpointTTL, LocalEndpointTTL and 7 d;
// a config endpoint on a private address gets 7 d.
func TestEndpointsTTLByAddressClass(t *testing.T) {
	configured, err := tcp.ParseEndpoint("10.9.9.9:1791")
	if err != nil {
		t.Fatalf("ParseEndpoint: %v", err)
	}
	mod := &Module{config: Config{ListenPort: 1791}, configEndpoints: []exonet.Endpoint{configured}}
	mod.IP = localIPs{ips: []ip.IP{mustIP(t, "192.168.1.20"), mustIP(t, "100.64.0.7"), mustIP(t, "203.0.113.5")}}

	want := map[string]time.Duration{
		"192.168.1.20:1791": ipmod.LocalEndpointTTL,
		"100.64.0.7:1791":   ipmod.LocalEndpointTTL,
		"203.0.113.5:1791":  7 * 24 * time.Hour,
		"10.9.9.9:1791":     7 * 24 * time.Hour,
	}
	got := mod.endpoints()
	if len(got) != len(want) {
		t.Fatalf("endpoints() = %v; want %v rows", got, len(want))
	}
	for _, e := range got {
		ttl, ok := want[e.Address()]
		if !ok || e.TTL == nil || time.Duration(*e.TTL)*time.Second != ttl {
			t.Errorf("%v has TTL %v s; want %v", e.Address(), e.TTL, ttl)
		}
	}
}
