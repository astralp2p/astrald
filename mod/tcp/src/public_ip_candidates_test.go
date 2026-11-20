package tcp

import (
	"net"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
)

// localIPs is an ip module whose interfaces hold a fixed set of addresses.
type localIPs struct {
	noLocalIPs
	ips []ip.IP
}

func (l localIPs) LocalIPs() ([]ip.IP, error) { return l.ips, nil }

func parseIPs(addrs ...string) (out []ip.IP) {
	for _, a := range addrs {
		out = append(out, ip.IP(net.ParseIP(a)))
	}
	return
}

func candidates(addrs ...string) []string {
	mod := &Module{Deps: Deps{IP: localIPs{ips: parseIPs(addrs...)}}}

	var out []string
	for _, c := range mod.PublicIPCandidates() {
		out = append(out, c.String())
	}
	return out
}

// TestPublicIPCandidatesDropsNonGlobalRanges holds that an interface address in
// a range that no peer can reach is not a public IP candidate, and that a
// documentation range, which labs and tests use as public, still is one.
func TestPublicIPCandidatesDropsNonGlobalRanges(t *testing.T) {
	cases := []struct {
		name string
		addr string
		kept bool
	}{
		{"this-network 0.0.0.0/8", "0.0.0.1", false},
		{"CGNAT 100.64.0.0/10 low edge", "100.64.0.0", false},
		{"CGNAT 100.64.0.0/10 high edge", "100.127.255.255", false},
		{"IPv4-mapped CGNAT", "::ffff:100.64.1.2", false},
		{"IETF protocol assignments 192.0.0.0/24 (464XLAT)", "192.0.0.8", false},
		{"benchmarking 198.18.0.0/15 low edge", "198.18.0.1", false},
		{"benchmarking 198.18.0.0/15 high edge", "198.19.255.255", false},
		{"reserved 240.0.0.0/4", "240.0.0.1", false},
		{"discard-only 100::/64", "100::1", false},
		{"just below CGNAT", "100.63.255.255", true},
		{"just above CGNAT", "100.128.0.0", true},
		{"just below benchmarking", "198.17.255.255", true},
		{"just above benchmarking", "198.20.0.0", true},
		{"public 31.61.247.94", "31.61.247.94", true},
		{"TEST-NET-1", "192.0.2.1", true},
		{"TEST-NET-2", "198.51.100.1", true},
		{"TEST-NET-3", "203.0.113.1", true},
		{"private 192.168.1.2", "192.168.1.2", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := candidates(tc.addr)
			if tc.kept && len(got) != 1 {
				t.Fatalf("candidates(%s) = %v; want the address kept", tc.addr, got)
			}
			if !tc.kept && len(got) != 0 {
				t.Fatalf("candidates(%s) = %v; want none", tc.addr, got)
			}
		})
	}
}

// TestPublicIPCandidatesKeepsPublicBesideCarrierInternal holds that a phone with
// a 464XLAT interface address and a public address lists only the public one.
func TestPublicIPCandidatesKeepsPublicBesideCarrierInternal(t *testing.T) {
	for _, order := range [][]string{
		{"192.0.0.8", "31.61.247.94"},
		{"31.61.247.94", "192.0.0.8"},
	} {
		if got, want := candidates(order...), []string{"31.61.247.94"}; !slices.Equal(got, want) {
			t.Fatalf("candidates(%v) = %v; want %v", order, got, want)
		}
	}
}
