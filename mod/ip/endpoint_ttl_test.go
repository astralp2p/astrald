package ip

import (
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
)

// TestEndpointTTL (criterion 1): a private, a CGNAT, a ULA and a public
// address get LocalEndpointTTL, LocalEndpointTTL, LocalEndpointTTL and 7 d.
func TestEndpointTTL(t *testing.T) {
	tests := []struct {
		addr string
		want time.Duration
	}{
		{"192.168.1.20", LocalEndpointTTL},
		{"10.0.0.4", LocalEndpointTTL},
		{"100.64.1.2", LocalEndpointTTL},
		{"fd00::1", LocalEndpointTTL},
		{"203.0.113.5", 7 * 24 * time.Hour},
		{"2001:4860::8888", 7 * 24 * time.Hour},
	}
	for _, test := range tests {
		a, err := ip.ParseIP(test.addr)
		if err != nil {
			t.Fatalf("ParseIP(%v): %v", test.addr, err)
		}
		if got := EndpointTTL(a); got != test.want {
			t.Errorf("EndpointTTL(%v) = %v; want %v", test.addr, got, test.want)
		}
	}
	if LocalEndpointTTL != 2*time.Hour {
		t.Fatalf("LocalEndpointTTL = %v; want 2h", LocalEndpointTTL)
	}
}
