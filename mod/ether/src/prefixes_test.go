package ether

import (
	"bytes"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
	ethermod "github.com/astralp2p/astrald/mod/ether"
)

// staticLookup returns a prefixLookup that answers the prefixes of fixed interface addresses.
func staticLookup(t *testing.T, cidrs ...string) prefixLookup {
	t.Helper()

	var addrs []net.Addr
	for _, c := range cidrs {
		addrs = append(addrs, cidrAddr(c))
	}
	prefixes := parsePrefixes(addrs)
	if len(prefixes) != len(cidrs) {
		t.Fatalf("parsePrefixes(%v) kept %v prefixes; want all", cidrs, len(prefixes))
	}

	return func(net.IP) ([]localPrefix, error) { return prefixes, nil }
}

func prefixStrings(prefixes []localPrefix) (out []string) {
	for _, p := range prefixes {
		if len(p.addr) != net.IPv4len {
			out = append(out, "non-4-byte "+p.addr.String())
			continue
		}
		out = append(out, p.addr.String()+" in "+p.subnet.String())
	}
	return
}

func TestParsePrefixes(t *testing.T) {
	mapped := &net.IPNet{IP: net.ParseIP("172.16.5.4"), Mask: net.CIDRMask(120, 128)}
	mobile := &net.IPNet{IP: net.ParseIP("10.1.2.3"), Mask: net.CIDRMask(8, 32)}

	got := parsePrefixes([]net.Addr{
		cidrAddr("192.168.1.10/24"),
		mobile,
		mapped,
		cidrAddr("127.0.0.1/8"),
		cidrAddr("169.254.3.4/16"),
		cidrAddr("2001:db8::1/64"),
		cidrAddr("fe80::1/64"),
		&net.IPAddr{IP: net.ParseIP("10.9.9.9")},
		&net.IPNet{},
		nil,
	})

	want := []string{"192.168.1.10 in 192.168.1.0/24", "10.1.2.3 in 10.0.0.0/8", "172.16.5.4 in 172.16.5.0/24"}
	if !slices.Equal(prefixStrings(got), want) {
		t.Errorf("parsePrefixes = %v; want %v", prefixStrings(got), want)
	}
}

type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time           { return c.at }
func (c *fakeClock) advance(by time.Duration) { c.at = c.at.Add(by) }

// TestPrefixCacheRefresh pins when the cache enumerates the interface addresses again:
// once stale, and on a miss no sooner than prefixMissRefresh after the last load.
func TestPrefixCacheRefresh(t *testing.T) {
	clock := &fakeClock{at: time.Unix(1_800_000_000, 0)}
	addrs := []net.Addr{cidrAddr("192.168.1.10/24")}
	loads := 0
	cache := &prefixCache{now: clock.now, addrs: func() ([]net.Addr, error) {
		loads++
		return addrs, nil
	}}

	onLink := net.ParseIP("192.168.1.20").To4()
	newLink := net.ParseIP("10.0.0.7").To4()

	steps := []struct {
		name      string
		advance   time.Duration
		src       net.IP
		wantLoads int
		wantLink  bool
	}{
		{"first lookup loads", 0, onLink, 1, true},
		{"a fresh hit reuses the cache", prefixMaxAge - time.Millisecond, onLink, 1, true},
		{"a stale cache reloads", time.Millisecond, onLink, 2, true},
		{"a miss right after a load waits", 0, newLink, 2, false},
		{"a later miss reloads", prefixMissRefresh, newLink, 3, true},
		{"the new subnet is cached", 0, newLink, 3, true},
	}

	for i, step := range steps {
		if i == 3 {
			addrs = append(addrs, cidrAddr("10.0.0.5/8"))
		}
		clock.advance(step.advance)

		prefixes, err := cache.lookup(step.src)
		if err != nil {
			t.Fatalf("%v: lookup: %v", step.name, err)
		}
		if loads != step.wantLoads {
			t.Errorf("%v: loads = %v; want %v", step.name, loads, step.wantLoads)
		}
		if got := isOnLink(prefixes, step.src); got != step.wantLink {
			t.Errorf("%v: %v on link = %v; want %v", step.name, step.src, got, step.wantLink)
		}
	}
}

func TestPrefixCacheReportsALoadError(t *testing.T) {
	boom := errors.New("interfaces unavailable")
	cache := &prefixCache{now: time.Now, addrs: func() ([]net.Addr, error) { return nil, boom }}

	if _, err := cache.lookup(net.ParseIP("192.168.1.20")); !errors.Is(err, boom) {
		t.Errorf("lookup = %v; want %v", err, boom)
	}
}

// note: 10.0.0.0/8 holds 10.1.0.0/16; the longest prefix wins.
var testLocal = []string{"192.168.1.10/24", "10.0.0.5/8", "10.1.0.9/16", "172.16.5.4/20"}

func TestSubnetOf(t *testing.T) {
	mod := &Module{lookupPrefixes: staticLookup(t, testLocal...)}

	tests := []struct {
		src    string
		want   string
		wantOK bool
	}{
		{"192.168.1.77", "192.168.1.0/24", true},
		{"192.168.1.10", "192.168.1.0/24", true},
		{"10.1.2.3", "10.1.0.0/16", true},
		{"10.2.0.1", "10.0.0.0/8", true},
		{"172.16.15.1", "172.16.0.0/20", true},
		{"192.168.2.1", "", false},
		{"169.254.1.1", "", false},
		{"127.0.0.1", "", false},
		{"fe80::1", "", false},
	}

	for _, tt := range tests {
		got, ok := mod.SubnetOf(ip.IP(net.ParseIP(tt.src)))
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("SubnetOf(%v) = (%q, %v); want (%q, %v)", tt.src, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestPushToSubnetOf(t *testing.T) {
	mod := &Module{lookupPrefixes: staticLookup(t, testLocal...)}
	obj := astral.NewString8("scan reply")
	packet := mustPacket(t, obj)

	tests := []struct {
		src     string
		want    string
		wantErr error
	}{
		{src: "192.168.1.77", want: "192.168.1.255"},
		{src: "10.1.2.3", want: "10.1.255.255"},
		{src: "10.2.0.1", want: "10.255.255.255"},
		{src: "172.16.15.1", want: "172.16.15.255"},
		{src: "192.168.2.1", wantErr: ethermod.ErrNotOnLink},
		{src: "fe80::1", wantErr: ethermod.ErrNotOnLink},
	}

	for _, tt := range tests {
		var sent []string
		send := func(target ip.IP, data []byte) (int, error) {
			if !bytes.Equal(data, packet) {
				t.Errorf("PushToSubnetOf(%v) sent %x; want %x", tt.src, data, packet)
			}
			sent = append(sent, net.IP(target).String())
			return len(data), nil
		}

		err := mod.pushToSubnetOf(ip.IP(net.ParseIP(tt.src)), obj, send)
		if !errors.Is(err, tt.wantErr) {
			t.Errorf("PushToSubnetOf(%v) = %v; want %v", tt.src, err, tt.wantErr)
		}

		var want []string
		if tt.want != "" {
			want = []string{tt.want}
		}
		if !slices.Equal(sent, want) {
			t.Errorf("PushToSubnetOf(%v) sent to %v; want %v", tt.src, sent, want)
		}
	}
}

// TestPushToSubnetOfUsesTheSocket pins that the exported method reaches the socket only for an on-link source.
func TestPushToSubnetOfUsesTheSocket(t *testing.T) {
	mod := &Module{lookupPrefixes: staticLookup(t, testLocal...)}
	obj := astral.NewString8("x")

	err := mod.PushToSubnetOf(ip.IP(net.ParseIP("192.168.1.77")), obj)
	if err == nil || err.Error() != "socket not initialized" {
		t.Errorf("PushToSubnetOf(on link) = %v; want \"socket not initialized\"", err)
	}

	if err := mod.PushToSubnetOf(ip.IP(net.ParseIP("192.168.2.1")), obj); !errors.Is(err, ethermod.ErrNotOnLink) {
		t.Errorf("PushToSubnetOf(off link) = %v; want %v", err, ethermod.ErrNotOnLink)
	}
}
