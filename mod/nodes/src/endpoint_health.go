package nodes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/astral"
)

// endpointHealthRetention is how long a health row stays after its last update.
const endpointHealthRetention = 30 * 24 * time.Hour

// dialLink dials endpoint, establishes a link to target over it and records
// the outcome; ctx is round or a context derived from it. A dial that ends
// because round is cancelled, as when another dial won or the callers left,
// is not recorded. A dial that the round budget ends is a failure.
// note: a handshake that reaches another identity is a failure.
func (mod *Module) dialLink(round, ctx *astral.Context, target *astral.Identity, endpoint exonet.Endpoint) (*Link, error) {
	// why: a row taken after its round ended is never dialed, so it has no outcome.
	if err := round.Err(); err != nil {
		return nil, err
	}

	start := time.Now()
	link, err := mod.dialAndEstablish(ctx, target, endpoint)
	if err == nil || !errors.Is(round.Err(), context.Canceled) {
		mod.recordDial(target, endpoint, err, time.Since(start))
	}
	return link, err
}

// dialAndEstablish dials endpoint and establishes a link to target over it.
func (mod *Module) dialAndEstablish(ctx *astral.Context, target *astral.Identity, endpoint exonet.Endpoint) (*Link, error) {
	conn, err := mod.Exonet.Dial(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	rawLink, err := mod.EstablishOutboundLink(ctx, target, conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("establish link: %w", err)
	}
	return rawLink.(*Link), nil
}

// recordDial stores the outcome of one dial of endpoint of target: a nil err
// is a success that took latency, any other err a failure.
// note: a module without a database, as in unit fixtures, keeps no history.
func (mod *Module) recordDial(target *astral.Identity, endpoint exonet.Endpoint, err error, latency time.Duration) {
	if mod.db == nil || endpoint == nil {
		return
	}

	now := time.Now().UTC()
	ref := healthRef{
		identity: target,
		endpoint: endpointKey{endpoint.Network(), endpoint.Address()},
		scope:    scopeOf(endpoint.Network(), endpoint.Address(), mod.networkFingerprint()),
	}
	var dbErr error
	if err == nil {
		dbErr = mod.db.RecordDialSuccess(ref, now, latency)
	} else {
		dbErr = mod.db.RecordDialFailure(ref, now)
	}
	if dbErr != nil {
		mod.log.Logv(1, "recording dial of %v at %v: %v", target, endpoint, dbErr)
	}
}

// scopeOf returns the Scope under which the dial history of an endpoint is
// kept: the dialer's network fingerprint for a non-public tcp or kcp endpoint,
// whose reachability depends on where the dialer is, else "" (public, onion and
// gateway endpoints reach the same from every network).
// An unknown fingerprint ("") keeps every endpoint under "".
func scopeOf(network, address, fingerprint string) string {
	if isLocalEndpoint(network, address) {
		return fingerprint
	}
	return ""
}

// healthSet is the health rows of one identity that matter on the current network.
type healthSet struct {
	fingerprint string
	// global holds the rows under Scope "".
	global map[endpointKey]*dbEndpointHealth
	// local holds the rows under the current fingerprint.
	local map[endpointKey]*dbEndpointHealth
}

// of returns the dial history of the endpoint from the current network, nil for
// none, and the days its subject listed it.
func (s *healthSet) of(network, address string) (dial *dbEndpointHealth, seenDays uint32) {
	key := endpointKey{network, address}
	if claim := s.global[key]; claim != nil {
		seenDays = claim.SeenDays
	}
	if scopeOf(network, address, s.fingerprint) != "" {
		return s.local[key], seenDays
	}
	return s.global[key], seenDays
}

// findEndpointHealth returns the health rows of target under Scope "" and the
// current fingerprint, from one query.
func (mod *Module) findEndpointHealth(target *astral.Identity) *healthSet {
	set := &healthSet{
		fingerprint: mod.networkFingerprint(),
		global:      map[endpointKey]*dbEndpointHealth{},
		local:       map[endpointKey]*dbEndpointHealth{},
	}
	if mod.db == nil {
		return set
	}

	rows, err := mod.db.FindEndpointHealth(target, "", set.fingerprint)
	if err != nil {
		mod.log.Logv(1, "finding endpoint health of %v: %v", target, err)
		return set
	}
	for _, row := range rows {
		if row.Scope == "" {
			set.global[endpointKey{row.Network, row.Address}] = row
		} else {
			set.local[endpointKey{row.Network, row.Address}] = row
		}
	}
	return set
}

// fingerprinter names the network this node is on (mod/ip).
type fingerprinter interface {
	NetworkFingerprint() string
}

// networkFingerprint returns the fingerprint of this node's network, and ""
// when it is unknown: without the ip module, or when listing the interfaces fails.
func (mod *Module) networkFingerprint() string {
	if mod.fingerprints == nil {
		return ""
	}
	return mod.fingerprints.NetworkFingerprint()
}

// subnetLister lists the subnets of this node's interfaces (mod/ip).
type subnetLister interface {
	LocalSubnets() ([]*net.IPNet, error)
}

// ownSubnets returns the subnets of this node's interfaces, and false when
// they are unknown: without the ip module, or when listing them fails.
func (mod *Module) ownSubnets() ([]*net.IPNet, bool) {
	if mod.subnets == nil {
		return nil, false
	}
	nets, err := mod.subnets.LocalSubnets()
	if err != nil {
		return nil, false
	}
	return nets, true
}

// endpointIP returns the IP of a tcp or kcp endpoint, and false for any other.
func endpointIP(network, address string) (ip.IP, bool) {
	if network != "tcp" && network != "kcp" {
		return nil, false
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return nil, false
	}
	return ip.IP(ap.Addr().Unmap().AsSlice()), true
}

// isLocalEndpoint reports whether a row is a tcp or kcp endpoint on a
// non-public IP.
func isLocalEndpoint(network, address string) bool {
	a, ok := endpointIP(network, address)
	return ok && !a.IsPublic()
}

// isOffSubnet reports whether a row is a tcp or kcp endpoint on a non-public,
// non-loopback IP outside every one of subnets.
func isOffSubnet(network, address string, subnets []*net.IPNet) bool {
	a, ok := endpointIP(network, address)
	if !ok || a.IsPublic() || a.IsLoopback() {
		return false
	}
	for _, n := range subnets {
		if n.Contains(net.IP(a)) {
			return false
		}
	}
	return true
}
