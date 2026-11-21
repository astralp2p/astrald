package ip

import (
	"errors"
	"net"
	"testing"
)

// fingerprintWith returns the NetworkFingerprint of a fresh module whose
// interfaces hold cidrs.
func fingerprintWith(t *testing.T, cidrs ...string) string {
	t.Helper()

	var addrs []net.Addr
	for _, c := range cidrs {
		addrs = append(addrs, cidr(t, c))
	}
	stubInterfaceAddrs(t, addrs, nil)
	return (&Module{}).NetworkFingerprint()
}

// TestNetworkFingerprintStableUnderInterfaceOrder (criterion 3): the same
// subnets in another interface order, a loopback address, a repeated subnet
// and another host address in the same subnet leave the fingerprint unchanged.
func TestNetworkFingerprintStableUnderInterfaceOrder(t *testing.T) {
	base := fingerprintWith(t, "192.168.1.2/24", "10.0.0.7/8", "2001:db8::5/64")
	if base == "" {
		t.Fatal("fingerprint is empty")
	}

	same := map[string][]string{
		"reordered":        {"2001:db8::5/64", "10.0.0.7/8", "192.168.1.2/24"},
		"with loopback":    {"127.0.0.1/8", "192.168.1.2/24", "10.0.0.7/8", "::1/128", "2001:db8::5/64"},
		"repeated subnet":  {"192.168.1.2/24", "192.168.1.9/24", "10.0.0.7/8", "2001:db8::5/64"},
		"other host in it": {"192.168.1.77/24", "10.0.0.8/8", "2001:db8::6/64"},
	}
	for name, cidrs := range same {
		if got := fingerprintWith(t, cidrs...); got != base {
			t.Errorf("fingerprint %v = %q; want %q", name, got, base)
		}
	}
}

// TestNetworkFingerprintChangesWithSubnet (criterion 3): another subnet, a
// subnet added or removed, and another mask each change the fingerprint.
func TestNetworkFingerprintChangesWithSubnet(t *testing.T) {
	base := fingerprintWith(t, "192.168.1.2/24", "10.0.0.7/8")

	other := map[string][]string{
		"moved subnet":   {"192.168.2.2/24", "10.0.0.7/8"},
		"removed subnet": {"192.168.1.2/24"},
		"added subnet":   {"192.168.1.2/24", "10.0.0.7/8", "172.16.0.3/16"},
		"changed mask":   {"192.168.1.2/16", "10.0.0.7/8"},
		"no subnets":     {},
	}
	seen := map[string]string{base: "base"}
	for name, cidrs := range other {
		got := fingerprintWith(t, cidrs...)
		if prev, dup := seen[got]; dup {
			t.Errorf("fingerprint %v = %q, the same as %v", name, got, prev)
		}
		seen[got] = name
	}
}

// TestNetworkFingerprintRecomputedOnChange (criterion 3): the fingerprint is
// cached until refreshFingerprint, which the address watcher calls, and a
// failed listing keeps the cached value.
func TestNetworkFingerprintRecomputedOnChange(t *testing.T) {
	stubInterfaceAddrs(t, []net.Addr{cidr(t, "192.168.1.2/24")}, nil)
	mod := &Module{}
	home := mod.NetworkFingerprint()

	stubInterfaceAddrs(t, []net.Addr{cidr(t, "10.0.0.7/24")}, nil)
	if got := mod.NetworkFingerprint(); got != home {
		t.Fatalf("fingerprint %q changed before a refresh; want the cached %q", got, home)
	}
	office := mod.refreshFingerprint()
	if office == home || mod.NetworkFingerprint() != office {
		t.Fatalf("fingerprint after a refresh %q, then %q; want one new value different from %q", office, mod.NetworkFingerprint(), home)
	}

	stubInterfaceAddrs(t, nil, errors.New("no interfaces"))
	if got := mod.refreshFingerprint(); got != office {
		t.Fatalf("fingerprint after a failed listing %q; want the cached %q", got, office)
	}
	if got := (&Module{}).NetworkFingerprint(); got != "" {
		t.Fatalf("fingerprint of a module that cannot list interfaces %q; want empty", got)
	}
}
