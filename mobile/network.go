package mobile

import (
	"net"

	ethersrc "github.com/astralp2p/astrald/mod/ether/src"
)

// broadcastInterfaces adapts the host's eligible LAN addresses to mod/ether.
// why: the host checks real interface flags; Go must not enumerate them again.
func broadcastInterfaces(cidrs string) []ethersrc.NetInterface {
	var addrs []net.Addr
	for _, addr := range parseCIDRList(cidrs) {
		network := addr.(*net.IPNet)
		ones, bits := network.Mask.Size()
		if network.IP.To4() == nil || !network.IP.IsGlobalUnicast() || bits != 32 || ones > 30 {
			continue
		}
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil
	}
	return []ethersrc.NetInterface{{
		Flags: net.FlagUp | net.FlagBroadcast,
		Addrs: func() ([]net.Addr, error) { return addrs, nil },
	}}
}
