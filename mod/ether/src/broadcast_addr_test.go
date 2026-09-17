package ether

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
)

type cidrAddr string

func (a cidrAddr) Network() string { return "ip+net" }
func (a cidrAddr) String() string  { return string(a) }

func stubNetInterfaces(t *testing.T, ifaces []NetInterface, err error) {
	t.Helper()

	orig := NetInterfaces
	t.Cleanup(func() { NetInterfaces = orig })
	NetInterfaces = func() ([]NetInterface, error) { return ifaces, err }
}

func staticAddrs(addrs ...string) func() ([]net.Addr, error) {
	return func() ([]net.Addr, error) {
		var out []net.Addr
		for _, a := range addrs {
			out = append(out, cidrAddr(a))
		}
		return out, nil
	}
}

func TestBroadcastAddr(t *testing.T) {
	tests := []struct {
		name string
		addr net.Addr
		want string
	}{
		{"class c", cidrAddr("192.168.1.10/24"), "192.168.1.255"},
		{"class a", cidrAddr("10.1.2.3/8"), "10.255.255.255"},
		{"host route", cidrAddr("172.16.5.4/32"), "172.16.5.4"},
		{"ipnet with 4-byte ip", &net.IPNet{IP: net.ParseIP("192.168.1.10").To4(), Mask: net.CIDRMask(24, 32)}, "192.168.1.255"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BroadcastAddr(tt.addr)
			if err != nil {
				t.Fatalf("BroadcastAddr(%v): %v", tt.addr, err)
			}
			if !got.Equal(net.ParseIP(tt.want)) {
				t.Errorf("BroadcastAddr(%v) = %v; want %v", tt.addr, got, tt.want)
			}
		})
	}

	if got, err := BroadcastAddr(cidrAddr("not-cidr")); err == nil {
		t.Errorf("BroadcastAddr(not-cidr) = (%v, nil); want an error", got)
	}
}

func TestIsLinkLocal(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"169.254.1.1", true},
		{"169.253.1.1", false},
		{"fe80::1", true},
		{"2001:db8::1", false},
		{"::ffff:169.254.0.1", true},
	}

	for _, tt := range tests {
		if got := IsLinkLocal(net.ParseIP(tt.addr)); got != tt.want {
			t.Errorf("IsLinkLocal(%v) = %v; want %v", tt.addr, got, tt.want)
		}
	}
}

func TestIsInterfaceEnabled(t *testing.T) {
	tests := []struct {
		name  string
		flags net.Flags
		want  bool
	}{
		{"up broadcast", net.FlagUp | net.FlagBroadcast, true},
		{"loopback", net.FlagUp | net.FlagBroadcast | net.FlagLoopback, false},
		{"down", net.FlagBroadcast, false},
		{"no broadcast", net.FlagUp, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInterfaceEnabled(NetInterface{Flags: tt.flags}); got != tt.want {
				t.Errorf("isInterfaceEnabled(%v) = %v; want %v", tt.flags, got, tt.want)
			}
		})
	}
}

func TestBroadcastSkipsLoopbackInterface(t *testing.T) {
	addrsCalled := false
	stubNetInterfaces(t, []NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast | net.FlagLoopback,
		Addrs: func() ([]net.Addr, error) {
			addrsCalled = true
			return nil, nil
		},
	}}, nil)

	if err := (&Module{}).broadcast([]byte("x")); err != nil {
		t.Errorf("broadcast() = %v; want nil", err)
	}
	if addrsCalled {
		t.Error("Addrs was called on a loopback interface; want skipped")
	}
}

func TestBroadcastSkipsLinkLocal(t *testing.T) {
	stubNetInterfaces(t, []NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast,
		Addrs: staticAddrs("169.254.3.4/16"),
	}}, nil)

	// note: a nil socket fails every write, so nil proves no write was attempted.
	if err := (&Module{}).broadcast([]byte("x")); err != nil {
		t.Errorf("broadcast() = %v; want nil", err)
	}
}

func TestBroadcastWithoutSocket(t *testing.T) {
	stubNetInterfaces(t, []NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast,
		Addrs: staticAddrs("192.168.1.10/24"),
	}}, nil)

	err := (&Module{}).broadcast([]byte("x"))
	if err == nil || !strings.Contains(err.Error(), "socket not initialized") {
		t.Errorf("broadcast() = %v; want \"socket not initialized\"", err)
	}
}

func TestBroadcastPropagatesErrors(t *testing.T) {
	addrsErr := errors.New("addrs failed")
	stubNetInterfaces(t, []NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast,
		Addrs: func() ([]net.Addr, error) { return nil, addrsErr },
	}}, nil)

	if err := (&Module{}).broadcast([]byte("x")); !errors.Is(err, addrsErr) {
		t.Errorf("broadcast() with failing Addrs = %v; want %v", err, addrsErr)
	}

	ifacesErr := errors.New("interfaces failed")
	stubNetInterfaces(t, nil, ifacesErr)

	if err := (&Module{}).broadcast([]byte("x")); !errors.Is(err, ifacesErr) {
		t.Errorf("broadcast() with failing NetInterfaces = %v; want %v", err, ifacesErr)
	}
}

func TestSocketNotInitialized(t *testing.T) {
	mod := &Module{}

	_, err := mod.writeToIP(ip.IP(net.ParseIP("192.168.1.255")), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "socket not initialized") {
		t.Errorf("writeToIP() = %v; want \"socket not initialized\"", err)
	}

	_, _, err = mod.readBroadcast()
	if err == nil || !strings.Contains(err.Error(), "socket not initialized") {
		t.Errorf("readBroadcast() = %v; want \"socket not initialized\"", err)
	}
}
