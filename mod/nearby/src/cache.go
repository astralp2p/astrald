package nearby

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nearby"
	"github.com/astralp2p/astral-go/astral"
)

// maxCacheEntries bounds the cache; a status from a new source IP is dropped while it is full.
//
// why: every source IP on the LAN can claim an entry, and forged source IPs are free.
const maxCacheEntries = 512

// expiryInterval is how often expired cache entries and nonces are removed.
const expiryInterval = 30 * time.Second

// entry is the cached status of one source IP. It is never changed once stored.
type entry struct {
	NodeID *astral.Identity // note: the verified node; nil for an unrecognised stealth status
	UserID *astral.Identity // note: nil when unknown
	IP     ip.IP
	Via    string // note: nearby.ViaHint or nearby.ViaProfile
	Seen   time.Time
	Status *nearby.StatusMessage // note: as received
}

// expired reports whether the entry is older than statusTTL at now.
func (e *entry) expired(now time.Time) bool {
	return now.Sub(e.Seen) > statusTTL
}

// remaining returns the life the entry has left at now.
func (e *entry) remaining(now time.Time) time.Duration {
	return e.Seen.Add(statusTTL).Sub(now)
}

// isNode reports whether the entry is the verified status of node.
func (e *entry) isNode(node *astral.Identity) bool {
	return e.NodeID != nil && e.NodeID.IsEqual(node)
}

// peerCache holds one entry per source IP, at most maxCacheEntries.
//
// why: a mutex, not sig.Map: the cap check, the move of a node's entry to its new IP and
// the PeerSeen decision read and write several keys in one step.
type peerCache struct {
	mu      sync.Mutex
	entries map[string]*entry
}

// hasRoom reports whether a status from ip can be stored.
func (c *peerCache) hasRoom(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.roomFor(ip)
}

// roomFor reports whether ip holds an entry or the cache is below its cap; c.mu is held.
func (c *peerCache) roomFor(ip string) bool {
	_, ok := c.entries[ip]
	return ok || len(c.entries) < maxCacheEntries
}

// storeUnrecognised stores an unrecognised stealth status in place of its IP's entry. It
// fails when the IP holds a live verified entry, or when the cache is full.
//
// why: an unrecognised status passed no check, so it never displaces a verified peer.
func (c *peerCache) storeUnrecognised(e *entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := e.IP.String()
	if old, ok := c.entries[key]; ok && old.NodeID != nil && !old.expired(e.Seen) {
		return errVerifiedHeld
	}
	if !c.roomFor(key) {
		return errCacheFull
	}
	c.put(key, e)
	return nil
}

// storeVerified stores the verified status of e.NodeID in place of its IP's entry and
// removes the node's entry at another IP. stored is false when the cache is full. isNew
// is true unless a live entry of the node at the same IP and with the same user was
// replaced: the node is new to the cache, moved to another IP, has a new user, or
// returns after its entry expired.
func (c *peerCache) storeVerified(e *entry) (isNew, stored bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := e.IP.String()
	known := false
	for k, old := range c.entries {
		switch {
		case !old.isNode(e.NodeID):
		case k == key:
			// why: a node claimed while cached keeps its IP and its live entry; its new
			// user is the news its peers of that user act on.
			known = !old.expired(e.Seen) && old.UserID.IsEqual(e.UserID)
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
func (c *peerCache) put(key string, e *entry) {
	if c.entries == nil {
		c.entries = make(map[string]*entry)
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
