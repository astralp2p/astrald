package ip

import (
	"errors"
	"net"
	"slices"
	"testing"
)

// hostAddrs is a host with a LAN, a public address, a VPN, docker/podman/libvirt bridges,
// and an IPv6 link-local address on most interfaces.
func hostAddrs(t *testing.T) map[string][]net.Addr {
	return map[string][]net.Addr{
		"lo":              {cidr(t, "127.0.0.1/8"), cidr(t, "::1/128")},
		"eth0":            {cidr(t, "192.168.1.2/24"), cidr(t, "fe80::1/64")},
		"eth1":            {cidr(t, "203.0.113.7/24"), cidr(t, "2001:db8::5/64")},
		"wg0":             {cidr(t, "10.8.0.2/24")},
		"br-lan":          {cidr(t, "192.168.8.1/24"), cidr(t, "fe80::8/64")},
		"docker0":         {cidr(t, "172.17.0.1/16"), cidr(t, "fe80::42/64")},
		"br-0123456789ab": {cidr(t, "172.18.0.1/16"), cidr(t, "fe80::43/64")},
		"podman0":         {cidr(t, "10.88.0.1/16"), cidr(t, "fe80::44/64")},
		"cni-podman0":     {cidr(t, "10.89.0.1/16")},
		"virbr0":          {cidr(t, "192.168.122.1/24")},
		"veth1a2b3c":      {cidr(t, "fe80::45/64")},
	}
}

func flatten(byIface map[string][]net.Addr) (out []net.Addr) {
	names := make([]string, 0, len(byIface))
	for n := range byIface {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		out = append(out, byIface[n]...)
	}
	return
}

func TestLocalIPsExcludesHostOnlyAndLinkLocal(t *testing.T) {
	byIface := hostAddrs(t)
	stubInterfaceAddrs(t, flatten(byIface), nil)
	stubNetInterfaceAddrs(t, byIface, nil)

	got, err := (&Module{}).LocalIPs()
	if err != nil {
		t.Fatalf("LocalIPs: %v", err)
	}

	gotS := ipStrings(got)
	slices.Sort(gotS)
	want := []string{"10.8.0.2", "192.168.1.2", "192.168.8.1", "2001:db8::5", "203.0.113.7"}
	if !slices.Equal(gotS, want) {
		t.Errorf("LocalIPs() = %v; want %v", gotS, want)
	}
}

func TestLocalIPsWithoutInterfaceNames(t *testing.T) {
	stubInterfaceAddrs(t, []net.Addr{
		cidr(t, "192.168.1.2/24"),
		cidr(t, "fe80::1/64"),
		cidr(t, "169.254.10.1/16"),
		cidr(t, "172.17.0.1/16"),
	}, nil)
	stubNetInterfaceAddrs(t, nil, errors.New("netlink blocked"))

	got, err := (&Module{}).LocalIPs()
	if err != nil {
		t.Fatalf("LocalIPs: %v", err)
	}

	// Only the IPv6 link-local rule applies when interface names are unavailable.
	want := []string{"192.168.1.2", "169.254.10.1", "172.17.0.1"}
	if gotS := ipStrings(got); !slices.Equal(gotS, want) {
		t.Errorf("LocalIPs() = %v; want %v", gotS, want)
	}
}

func TestLocalAddressesKeepsHostOnly(t *testing.T) {
	byIface := hostAddrs(t)
	stubInterfaceAddrs(t, flatten(byIface), nil)
	stubNetInterfaceAddrs(t, byIface, nil)

	all, err := (&Module{}).localAddresses(false)
	if err != nil {
		t.Fatalf("localAddresses: %v", err)
	}
	gotS := ipStrings(all)
	for _, s := range []string{"172.17.0.1", "fe80::42"} {
		if !slices.Contains(gotS, s) {
			t.Errorf("localAddresses(false) = %v; missing %s", gotS, s)
		}
	}
}

func TestIsHostOnlyIface(t *testing.T) {
	for name, want := range map[string]bool{
		"docker0":         true,
		"docker_gwbridge": true,
		"br-0123456789ab": true,
		"podman1":         true,
		"cni0":            true,
		"virbr0":          true,
		"vethabc":         true,
		"lxdbr0":          true,
		"vboxnet0":        true,
		"vmnet8":          true,
		"eth0":            false,
		"wlan0":           false,
		"br-lan":          false,
		"br0":             false,
		"wg0":             false,
		"tailscale0":      false,
	} {
		if got := isHostOnlyIface(name); got != want {
			t.Errorf("isHostOnlyIface(%q) = %v; want %v", name, got, want)
		}
	}
}
