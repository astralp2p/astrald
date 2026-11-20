package nodes

import (
	"net"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/exonet"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/kcp"
	"github.com/astralp2p/astral-go/api/tcp"
)

// linkWakeDelay is the wait between a local address change and the wake of the links bound to a removed address.
const linkWakeDelay = 500 * time.Millisecond

// scheduleLinkWake adds the addresses e removed to the pending set and starts
// the wake wait unless one is running. An event that removes nothing does nothing.
// why: a link bound to a removed address is dead, so one ping after the wait
// closes it within its ping timeout; an address that only appeared kills no link.
// why: a change during the wait joins it instead of restarting it, so a flapping
// interface costs one ping per matching link per wait.
func (mod *Module) scheduleLinkWake(e *ip.EventNetworkAddressChanged) {
	if len(e.Removed) == 0 {
		return
	}

	mod.wakeMu.Lock()
	mod.wakeIPs = append(mod.wakeIPs, e.Removed...)
	mod.wakeMu.Unlock()

	if !mod.wakePending.CompareAndSwap(false, true) {
		return
	}

	time.AfterFunc(mod.linkWakeDelay, func() {
		// note: cleared before the take, so an address removed during the wake starts a new wait.
		mod.wakePending.Store(false)
		mod.wakeLinks()
	})
}

// wakeLinks wakes the links bound to the addresses that the pending set holds, and empties the set.
func (mod *Module) wakeLinks() {
	mod.wakeMu.Lock()
	ips := mod.wakeIPs
	mod.wakeIPs = nil
	mod.wakeMu.Unlock()

	if mod.linkPool == nil || mod.linkPool.closed.Load() {
		return
	}

	mod.linkPool.WakeByLocalIP(ips)
}

// localIPIn reports whether the local endpoint ep holds one of ips.
// why: a nil endpoint, a non-IP endpoint, and a wildcard endpoint hold no
// concrete address, so none of them equals a removed one.
func localIPIn(ep exonet.Endpoint, ips []ip.IP) bool {
	local := localIP(ep)
	if local == nil {
		return false
	}

	return slices.ContainsFunc(ips, func(a ip.IP) bool { return net.IP(local).Equal(net.IP(a)) })
}

// localIP returns the concrete address of an IP endpoint, or nil for a nil, a
// non-IP, or a wildcard endpoint.
func localIP(ep exonet.Endpoint) ip.IP {
	var a ip.IP
	switch e := ep.(type) {
	case *tcp.Endpoint:
		if e != nil {
			a = e.IP
		}
	case *kcp.Endpoint:
		if e != nil {
			a = e.IP
		}
	}

	if len(a) == 0 || net.IP(a).IsUnspecified() {
		return nil
	}
	return a
}
