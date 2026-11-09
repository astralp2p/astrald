package mobile

// Host is the optional bridge from the daemon to platform system services.
// The Android wrapper implements this in Kotlin/Java; the methods are
// invoked from arbitrary Go goroutines, so implementations must be safe
// to call concurrently. gomobile handles JNI thread attach.
//
// All methods are best-effort. The daemon must remain functional with a
// nil Host — Host hooks gate platform-specific correctness (e.g. holding
// a multicast lock so the kernel doesn't drop inbound LAN broadcasts),
// not core operation.
type Host interface {
	// LANDiscoveryActive is called when mod/ether starts (true) and stops
	// (false) its UDP broadcast receiver. On Android, the implementation
	// should acquire a WifiManager.MulticastLock while active is true and
	// release it when active is false; without the lock, the Wi-Fi driver
	// silently drops inbound directed broadcasts on most devices.
	//
	// Calls are paired and serialized by the daemon's module lifecycle.
	LANDiscoveryActive(active bool)

	// LocalInterfaceAddrs returns the host's local IP addresses in CIDR
	// notation, one per line ("192.168.1.10/24", "fe80::1/64", ...). Used
	// to bypass Android's block on Go's netlink-based net.InterfaceAddrs.
	//
	// Empty string is interpreted as "no addresses available" — the daemon
	// proceeds with no advertised endpoints, no errors. Called repeatedly
	// (every ~3s by mod/ip's address-change watcher), so implementations
	// should be cheap; getifaddrs-based enumeration is fine.
	LocalInterfaceAddrs() string

	// LANBroadcastAddrs returns IPv4 CIDRs on active broadcast-capable
	// interfaces, one per line. Exclude loopback and point-to-point links.
	// why: net.Interfaces also uses netlink, independently of address lookup.
	// An empty result means no LAN is currently available.
	LANBroadcastAddrs() string
}
