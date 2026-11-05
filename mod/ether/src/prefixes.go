package ether

import (
	"net"
	"sync"
	"time"

	ipsrc "github.com/astralp2p/astrald/mod/ip/src"
)

// prefixMaxAge bounds how long the cached local prefixes are trusted.
const prefixMaxAge = 3 * time.Second

// prefixMissRefresh is the shortest interval between two refreshes that a lookup miss triggers.
// why: a flood of datagrams from sources outside every subnet must not become a flood of interface enumerations.
const prefixMissRefresh = time.Second

// localPrefix is one IPv4 address of the node with the subnet it sits on.
type localPrefix struct {
	addr   net.IP     // note: 4 bytes
	subnet *net.IPNet // note: 4-byte network and mask
}

// prefixLookup returns the local prefixes that decide about src.
type prefixLookup func(src net.IP) ([]localPrefix, error)

// prefixCache caches the local IPv4 prefixes.
type prefixCache struct {
	now   func() time.Time
	addrs func() ([]net.Addr, error)

	// why: one lock covers the load time, the prefixes and the load error, so a lookup never mixes two loads.
	mu       sync.Mutex
	loadedAt time.Time
	prefixes []localPrefix
	err      error
}

func newPrefixCache() *prefixCache {
	return &prefixCache{
		now: time.Now,
		// why: mobile/node.go replaces ipsrc.InterfaceAddrs after Load on Android, so every refresh reads the seam anew.
		addrs: func() ([]net.Addr, error) { return ipsrc.InterfaceAddrs() },
	}
}

// lookup returns the cached prefixes, refreshed first when they are stale or when no cached subnet contains src.
func (c *prefixCache) lookup(src net.IP) ([]localPrefix, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	age := c.now().Sub(c.loadedAt)
	if age >= prefixMaxAge || (age >= prefixMissRefresh && !isOnLink(c.prefixes, src)) {
		c.refresh()
	}

	return c.prefixes, c.err
}

func (c *prefixCache) refresh() {
	addrs, err := c.addrs()
	c.loadedAt = c.now()
	c.prefixes = parsePrefixes(addrs)
	c.err = err
}

// parsePrefixes returns the IPv4 prefixes of interface addresses.
// note: loopback and link-local (169.254/16) addresses are not on any ether link.
func parsePrefixes(addrs []net.Addr) (out []localPrefix) {
	for _, addr := range addrs {
		if addr == nil {
			continue
		}

		ip, subnet, err := net.ParseCIDR(addr.String())
		if err != nil || len(subnet.Mask) != net.IPv4len {
			continue
		}

		ip = ip.To4()
		if ip.IsLoopback() || IsLinkLocal(ip) {
			continue
		}

		out = append(out, localPrefix{addr: ip, subnet: subnet})
	}

	return
}

// findSubnet returns the local subnet that contains ip.
// why: the longest prefix wins, as in routing, so nested subnets resolve to the most specific link.
func findSubnet(prefixes []localPrefix, ip net.IP) (*net.IPNet, bool) {
	var best *net.IPNet
	var bestOnes = -1

	for _, p := range prefixes {
		ones, _ := p.subnet.Mask.Size()
		if ones > bestOnes && p.subnet.Contains(ip) {
			best, bestOnes = p.subnet, ones
		}
	}

	return best, best != nil
}

func isOnLink(prefixes []localPrefix, ip net.IP) bool {
	_, ok := findSubnet(prefixes, ip)
	return ok
}

func isOwnIP(prefixes []localPrefix, ip net.IP) bool {
	for _, p := range prefixes {
		if p.addr.Equal(ip) {
			return true
		}
	}

	return false
}
