package ether

import (
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/api/ip"
)

type cidrAddr string

func (a cidrAddr) Network() string { return "ip+net" }
func (a cidrAddr) String() string  { return string(a) }

func TestBroadcastAddrIsIPv4Only(t *testing.T) {
	tests := []struct {
		cidr    string
		want    string
		wantErr error
	}{
		{cidr: "192.168.1.10/24", want: "192.168.1.255"},
		{cidr: "10.1.2.3/8", want: "10.255.255.255"},
		{cidr: "172.16.5.4/20", want: "172.16.15.255"},
		{cidr: "192.0.2.1/32", want: "192.0.2.1"},
		{cidr: "2001:db8::1/64", wantErr: ErrNotIPv4},
		{cidr: "fd00::1/48", wantErr: ErrNotIPv4},
		{cidr: "fe80::1/64", wantErr: ErrNotIPv4},
	}
	for _, tt := range tests {
		t.Run(tt.cidr, func(t *testing.T) {
			got, err := BroadcastAddr(cidrAddr(tt.cidr))
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("BroadcastAddr(%s) = %v, %v; want error %v", tt.cidr, got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("BroadcastAddr(%s) error: %v", tt.cidr, err)
			}
			if len(got) != net.IPv4len || got.String() != tt.want {
				t.Errorf("BroadcastAddr(%s) = %v (len %d), want %s", tt.cidr, got, len(got), tt.want)
			}
		})
	}
}

func TestIsLinkLocal(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{ip: "169.254.1.1", want: true},
		{ip: "169.253.1.1", want: false},
		{ip: "::ffff:169.254.0.1", want: true},
		{ip: "192.168.1.255", want: false},
		{ip: "fe80::1", want: true},
		{ip: "febf::1", want: true},
		{ip: "fec0::1", want: false},
		{ip: "2001:db8::1", want: false},
	}
	for _, tt := range tests {
		if got := IsLinkLocal(net.ParseIP(tt.ip)); got != tt.want {
			t.Errorf("IsLinkLocal(%s) = %v, want %v", tt.ip, got, tt.want)
		}
	}
}

// TestBroadcastSkipsIPv6Prefixes checks that broadcast() skips IPv6 prefixes
// instead of aborting the interface loop. The socket is nil, so any attempted
// send returns an error.
func TestBroadcastSkipsIPv6Prefixes(t *testing.T) {
	prev := NetInterfaces
	t.Cleanup(func() { NetInterfaces = prev })
	NetInterfaces = func() ([]NetInterface, error) {
		return []NetInterface{{
			Flags: net.FlagUp | net.FlagBroadcast,
			Addrs: func() ([]net.Addr, error) {
				return []net.Addr{cidrAddr("2001:db8::1/64"), cidrAddr("fd00::1/48")}, nil
			},
		}}, nil
	}

	mod := &Module{}
	if err := broadcast([]byte("x"), mod.writeToIP); err != nil {
		t.Fatalf("broadcast with only IPv6 prefixes: %v", err)
	}
}

// note: the first target fails; the later targets on both interfaces still receive the packet.
func TestBroadcastContinuesAfterFailedSend(t *testing.T) {
	stubNetInterfaces(t, []NetInterface{
		{Flags: net.FlagUp | net.FlagBroadcast, Addrs: staticAddrs("192.168.1.10/24", "10.1.2.3/8")},
		{Flags: net.FlagUp | net.FlagBroadcast, Addrs: staticAddrs("172.16.5.4/20")},
	}, nil)

	errSend := errors.New("send failed")
	var sent []string
	send := func(target ip.IP, data []byte) (int, error) {
		if net.IP(target).String() == "192.168.1.255" {
			return 0, errSend
		}
		sent = append(sent, net.IP(target).String())
		return len(data), nil
	}

	err := broadcast([]byte("x"), send)
	if !errors.Is(err, errSend) {
		t.Errorf("broadcast error = %v, want %v", err, errSend)
	}

	want := []string{"10.255.255.255", "172.16.15.255"}
	if !slices.Equal(sent, want) {
		t.Errorf("sent to %v, want %v", sent, want)
	}
}
