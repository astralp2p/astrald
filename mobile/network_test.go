package mobile

import (
	"net"
	"testing"

	ethersrc "github.com/astralp2p/astrald/mod/ether/src"
)

func TestHostBroadcastAddresses(t *testing.T) {
	ifaces := broadcastInterfaces("10.0.0.101/24\n192.168.10.7/16\n")
	if len(ifaces) != 1 {
		t.Fatalf("expected LAN interface, got %v", len(ifaces))
	}
	iface := ifaces[0]
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 || iface.Flags&net.FlagLoopback != 0 {
		t.Fatalf("interface cannot broadcast: %v", iface.Flags)
	}
	addrs, err := iface.Addrs()
	if err != nil || len(addrs) != 2 {
		t.Fatalf("unexpected LAN addresses: %v, %v", addrs, err)
	}
	for i, want := range []string{"10.0.0.255", "192.168.255.255"} {
		got, err := ethersrc.BroadcastAddr(addrs[i])
		if err != nil || got.String() != want {
			t.Errorf("broadcast destination = %v, %v; want %v", got, err, want)
		}
	}
}

func TestHostBroadcastAddressesRejectNonLAN(t *testing.T) {
	for _, cidr := range []string{
		"", "invalid", "127.0.0.1/8", "169.254.1.2/16", "0.0.0.0/0",
		"10.0.1.101/32", "10.0.1.100/31", "224.0.0.1/24",
		"fe80::1%wlan0/64", "2001:db8::1/64",
	} {
		t.Run(cidr, func(t *testing.T) {
			if got := broadcastInterfaces(cidr); len(got) != 0 {
				t.Fatalf("unexpected broadcast interface for %v", cidr)
			}
		})
	}
}
