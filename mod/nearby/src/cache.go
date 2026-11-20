package nearby

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// maxCacheEntries bounds the cache; a status under a new key (source IP and tcp listen port)
// is dropped while it is full.
//
// why: every source IP on the LAN can claim an entry per port, and forged source IPs and ports are free.
const maxCacheEntries = 512

// expiryInterval is how often expired cache entries and nonces are removed.
const expiryInterval = 30 * time.Second

// entry is the cached status of one node, or of one unrecognised stealth status, at one
// source IP. It is never changed once stored.
type entry struct {
	NodeID *astral.Identity // note: the verified node; nil for an unrecognised stealth status
	UserID *astral.Identity // note: nil when unknown
	IP     ip.IP
	Via    string // note: nearby.ViaHint or nearby.ViaProfile
	Seen   time.Time
	Status *nearby.StatusMessage // note: as received
	Ports  *nearby.ListenPorts   // note: nil when the status carries no ports
}

// expired reports whether the entry is older than statusTTL at now.
func (e *entry) expired(now time.Time) bool {
	return now.Sub(e.Seen) > statusTTL
}

// remaining returns the life the entry has left at now.
func (e *entry) remaining(now time.Time) time.Duration {
	return e.Seen.Add(statusTTL).Sub(now)
}

// listenPorts returns the tcp and kcp ports the entry's node is dialed on: the ports its
// status carried, else the defaults.
func (e *entry) listenPorts() (tcp, kcp astral.Uint16) {
	if e.Ports == nil {
		return nearby.DefaultTCPPort, nearby.DefaultKCPPort
	}
	return e.Ports.TCP, e.Ports.KCP
}

// cacheKey identifies a cache entry: the source IP of its status and the tcp listen port the
// entry resolves to.
//
// why: nodes on one host share a source IP and listen on different ports, so the IP alone
// would let each broadcast evict the other node's entry.
// note: a status that announces tcp 0 (tcp off) keys on port 0, so two such nodes on one
// host share a key.
type cacheKey struct {
	ip   string
	port astral.Uint16
}

// keyOf returns the key of e: its source IP and its tcp listen port, 1791 when its status
// carries no ports. An unrecognised stealth status carries none.
func keyOf(e *entry) cacheKey {
	tcp, _ := e.listenPorts()
	return cacheKey{ip: e.IP.String(), port: tcp}
}

// isNode reports whether the entry is the verified status of node.
func (e *entry) isNode(node *astral.Identity) bool {
	return e.NodeID != nil && e.NodeID.IsEqual(node)
}

// peerCache holds one entry per cacheKey (source IP and tcp listen port), at most
// maxCacheEntries.
//
// why: a mutex, not sig.Map: the cap check, the move of a node's entry to its new key and
// the PeerSeen decision read and write several keys in one step.
type peerCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*entry
}

// hasRoom reports whether a status from ip can possibly be stored: the cache is below its
// cap, or ip holds an entry. It is the receive path's cheap check before identification,
// when the status's port is not known.
//
// why: the exact key is known only after identification, so the exact check is roomFor in
// the store; a status that passes here and fails there costs one identification, and a
// full cache turns a status from an IP without an entry away before any signature check.
func (c *peerCache) hasRoom(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) < maxCacheEntries {
		return true
	}
	for key := range c.entries {
		if key.ip == ip {
			return true
		}
	}
	return false
}

// roomFor reports whether key holds an entry or the cache is below its cap; c.mu is held.
func (c *peerCache) roomFor(key cacheKey) bool {
	_, ok := c.entries[key]
	return ok || len(c.entries) < maxCacheEntries
}

// storeUnrecognised stores an unrecognised stealth status in place of the entry under its
// key, the source IP and the default tcp port. It fails when that key holds a live verified
// entry, or when the cache is full and the key is new.
//
// why: an unrecognised status passed no check, so it never displaces a verified peer; a
// verified node on another port at the same IP keeps its entry, and the status takes the
// default-port key beside it.
func (c *peerCache) storeUnrecognised(e *entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := keyOf(e)
	if old, ok := c.entries[key]; ok && old.NodeID != nil && !old.expired(e.Seen) {
		return errVerifiedHeld
	}
	if !c.roomFor(key) {
		return errCacheFull
	}
	c.put(key, e)
	return nil
}

// samePorts reports whether a and b are dialed on the same tcp and kcp ports. A status
// without ports and one that carries the defaults are the same.
func samePorts(a, b *entry) bool {
	atcp, akcp := a.listenPorts()
	btcp, bkcp := b.listenPorts()
	return atcp == btcp && akcp == bkcp
}

// storeVerified stores the verified status of e.NodeID in place of the entry under its key
// and removes the node's entry under any other key, so a node holds at most one entry.
// stored is false when the cache is full and the key is new. isNew is true unless a live
// entry of the node under the same key, with the same user and the same listen ports, was
// replaced: the node is new to the cache, moved to another IP or tcp port, has a new user,
// has another kcp port, or returns after its entry expired.
func (c *peerCache) storeVerified(e *entry) (isNew, stored bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := keyOf(e)
	known := false
	for k, old := range c.entries {
		switch {
		case !old.isNode(e.NodeID):
		case k == key:
			// why: a node claimed while cached keeps its IP and its live entry; its new
			// user is the news its peers of that user act on.
			// why: a node restarted on another kcp port keeps its key and its live entry;
			// the ports resolve returns changed, and linking wakes at this status, not at
			// the next link task tick. A node on another tcp port has another key: its old
			// entry is removed below and it is new. The ports compared are the effective
			// ones, so a status without ports and one with 1791 and 1792 are not a change.
			known = !old.expired(e.Seen) && old.UserID.IsEqual(e.UserID) && samePorts(old, e)
		default:
			delete(c.entries, k)
		}
	}

	if !c.roomFor(key) {
		return false, false
	}
	c.put(key, e)
	return !known, true
}

// put stores e under key; c.mu is held.
func (c *peerCache) put(key cacheKey, e *entry) {
	if c.entries == nil {
		c.entries = make(map[cacheKey]*entry)
	}
	c.entries[key] = e
}

// live returns the entries that have not expired at now.
func (c *peerCache) live(now time.Time) []*entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	list := make([]*entry, 0, len(c.entries))
	for _, e := range c.entries {
		if !e.expired(now) {
			list = append(list, e)
		}
	}
	return list
}

// liveNodes returns the nodes of the verified entries that have not expired at now.
func (c *peerCache) liveNodes(now time.Time) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	nodes := make(map[string]bool, len(c.entries))
	for _, e := range c.entries {
		if e.NodeID != nil && !e.expired(now) {
			nodes[e.NodeID.String()] = true
		}
	}
	return nodes
}

// removeExpired removes the entries that expired at now.
func (c *peerCache) removeExpired(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, e := range c.entries {
		if e.expired(now) {
			delete(c.entries, k)
		}
	}
}

// runExpiry removes expired cache entries and nonces every expiryInterval until ctx ends.
func (mod *Module) runExpiry(ctx *astral.Context) {
	for {
		select {
		case <-mod.after(expiryInterval):
			now := mod.now()
			mod.cache.removeExpired(now)
			mod.nonces.removeExpired(now)
		case <-ctx.Done():
			return
		}
	}
}
