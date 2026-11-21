package ip

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"slices"
	"strings"
)

// fingerprintBytes is how many bytes of the hash a fingerprint keeps.
const fingerprintBytes = 8

// NetworkFingerprint returns a short hash of this node's sorted non-loopback
// subnets: the same on every call while the node stays on one network, and
// different on another. It is empty when the interfaces cannot be listed.
//
// why: dial history depends on where the dialer is, and the subnets are what
// tells a home from an office. A default gateway is not part of it, because the
// Go standard library has no portable way to read one (Android included).
// note: the fingerprint is recomputed when the watched addresses change, so a
// change of mask alone, with the same addresses, is seen only at the next change.
func (mod *Module) NetworkFingerprint() string {
	if fp := mod.fingerprint.Load(); fp != nil {
		return *fp
	}
	return mod.refreshFingerprint()
}

// refreshFingerprint recomputes the fingerprint from the interfaces, caches it
// and returns it. A failed listing keeps the cached value and returns it.
func (mod *Module) refreshFingerprint() string {
	nets, err := mod.LocalSubnets()
	if err != nil {
		if fp := mod.fingerprint.Load(); fp != nil {
			return *fp
		}
		return ""
	}

	fp := fingerprintOf(nets)
	mod.fingerprint.Store(&fp)
	return fp
}

// fingerprintOf hashes the network address and mask of every subnet in nets.
// The order of nets and repeated subnets do not change the result.
func fingerprintOf(nets []*net.IPNet) string {
	subnets := make([]string, 0, len(nets))
	for _, n := range nets {
		masked := net.IPNet{IP: n.IP.Mask(n.Mask), Mask: n.Mask}
		subnets = append(subnets, masked.String())
	}
	slices.Sort(subnets)
	subnets = slices.Compact(subnets)

	sum := sha256.Sum256([]byte(strings.Join(subnets, "\n")))
	return hex.EncodeToString(sum[:fingerprintBytes])
}
