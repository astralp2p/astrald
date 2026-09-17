package ip

import (
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
)

func stubInterfaceAddrs(t *testing.T, addrs []net.Addr, err error) {
	t.Helper()

	orig := InterfaceAddrs
	t.Cleanup(func() { InterfaceAddrs = orig })
	InterfaceAddrs = func() ([]net.Addr, error) { return addrs, err }
}

func cidr(t *testing.T, s string) *net.IPNet {
	t.Helper()

	addr, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR(%q): %v", s, err)
	}
	ipnet.IP = addr
	return ipnet
}

func ipStrings(ips []ip.IP) (out []string) {
	for _, i := range ips {
		out = append(out, i.String())
	}
	return
}

func TestLocalAddresses(t *testing.T) {
	stubInterfaceAddrs(t, []net.Addr{
		cidr(t, "127.0.0.1/8"),
		cidr(t, "192.168.1.2/24"),
		cidr(t, "::1/128"),
		cidr(t, "2001:db8::5/64"),
		&net.IPAddr{IP: net.ParseIP("10.0.0.1")},
		&net.IPNet{IP: nil},
	}, nil)

	mod := &Module{}

	local, err := mod.LocalIPs()
	if err != nil {
		t.Fatalf("LocalIPs: %v", err)
	}
	if got, want := ipStrings(local), []string{"192.168.1.2", "2001:db8::5"}; !slices.Equal(got, want) {
		t.Errorf("LocalIPs() = %v; want %v", got, want)
	}

	all, err := mod.localAddresses(true)
	if err != nil {
		t.Fatalf("localAddresses(true): %v", err)
	}
	if got, want := ipStrings(all), []string{"127.0.0.1", "192.168.1.2", "::1", "2001:db8::5"}; !slices.Equal(got, want) {
		t.Errorf("localAddresses(true) = %v; want %v", got, want)
	}
}

func TestLocalIPsPropagatesError(t *testing.T) {
	boom := errors.New("boom")
	stubInterfaceAddrs(t, nil, boom)

	got, err := (&Module{}).LocalIPs()
	if !errors.Is(err, boom) || got != nil {
		t.Errorf("LocalIPs() = (%v, %v); want (nil, %v)", got, err, boom)
	}
}

type staticCandidates struct {
	ips []ip.IP
}

func (p *staticCandidates) PublicIPCandidates() []ip.IP { return p.ips }

func parseIPs(ss ...string) (out []ip.IP) {
	for _, s := range ss {
		out = append(out, ip.IP(net.ParseIP(s)))
	}
	return
}

func TestPublicIPCandidatesNoProviders(t *testing.T) {
	if got := (&Module{}).PublicIPCandidates(); len(got) != 0 {
		t.Errorf("PublicIPCandidates() = %v; want empty", got)
	}
}

func TestPublicIPCandidatesDeduplicatesInOrder(t *testing.T) {
	mod := &Module{}
	p1 := &staticCandidates{ips: parseIPs("8.8.8.8", "1.1.1.1")}
	p2 := &staticCandidates{ips: parseIPs("1.1.1.1", "9.9.9.9")}

	for _, p := range []*staticCandidates{p1, p2} {
		if err := mod.AddPublicIPCandidateProvider(p); err != nil {
			t.Fatalf("AddPublicIPCandidateProvider: %v", err)
		}
	}

	want := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9"}
	if got := ipStrings(mod.PublicIPCandidates()); !slices.Equal(got, want) {
		t.Errorf("PublicIPCandidates() = %v; want %v", got, want)
	}

	if err := mod.AddPublicIPCandidateProvider(p1); err == nil {
		t.Error("adding the same provider twice returned nil error; want an error")
	}
	if got := ipStrings(mod.PublicIPCandidates()); !slices.Equal(got, want) {
		t.Errorf("PublicIPCandidates() after duplicate add = %v; want %v", got, want)
	}
}

func TestJoinIPs(t *testing.T) {
	if got := joinIPs(nil); got != "" {
		t.Errorf("joinIPs(nil) = %q; want empty", got)
	}
	if got, want := joinIPs(parseIPs("1.2.3.4", "::1")), "1.2.3.4, ::1"; got != want {
		t.Errorf("joinIPs = %q; want %q", got, want)
	}
}
