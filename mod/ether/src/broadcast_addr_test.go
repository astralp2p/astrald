package ether

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
)

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

func TestBroadcastAddrOfAnIPNet(t *testing.T) {
	addr := &net.IPNet{IP: net.ParseIP("192.168.1.10").To4(), Mask: net.CIDRMask(24, 32)}

	got, err := BroadcastAddr(addr)
	if err != nil {
		t.Fatalf("BroadcastAddr(%v): %v", addr, err)
	}
	if want := net.ParseIP("192.168.1.255"); !got.Equal(want) {
		t.Errorf("BroadcastAddr(%v) = %v; want %v", addr, got, want)
	}
}

func TestBroadcastAddrRejectsANonCIDR(t *testing.T) {
	if got, err := BroadcastAddr(cidrAddr("not-cidr")); err == nil {
		t.Errorf("BroadcastAddr(not-cidr) = (%v, nil); want an error", got)
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
