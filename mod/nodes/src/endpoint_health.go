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
	key := endpointKey{endpoint.Network(), endpoint.Address()}
	var dbErr error
	if err == nil {
		dbErr = mod.db.RecordDialSuccess(target, key, now, latency)
	} else {
		dbErr = mod.db.RecordDialFailure(target, key, now)
	}
	if dbErr != nil {
		mod.log.Logv(1, "recording dial of %v at %v: %v", target, endpoint, dbErr)
	}
}

// findEndpointHealth returns the global health rows of target by endpoint.
func (mod *Module) findEndpointHealth(target *astral.Identity) map[endpointKey]*dbEndpointHealth {
	out := map[endpointKey]*dbEndpointHealth{}
	if mod.db == nil {
		return out
	}

	rows, err := mod.db.FindEndpointHealth(target)
	if err != nil {
		mod.log.Logv(1, "finding endpoint health of %v: %v", target, err)
		return out
	}
	for _, row := range rows {
		out[endpointKey{row.Network, row.Address}] = row
	}
	return out
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
