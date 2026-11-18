package ip

import "net"

// InterfaceAddrs is an indirection over net.InterfaceAddrs to allow substitution in tests.
var InterfaceAddrs = net.InterfaceAddrs

// NetInterfaceAddrs returns the addresses of each local interface keyed by interface name.
// It is an indirection to allow substitution in tests.
var NetInterfaceAddrs = defaultNetInterfaceAddrs

func defaultNetInterfaceAddrs() (map[string][]net.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	out := make(map[string][]net.Addr, len(ifaces))
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		out[iface.Name] = addrs
	}

	return out, nil
}
