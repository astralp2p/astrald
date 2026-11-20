package nodes

import (
	"bytes"
	"errors"
	"time"

	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// zeroKey is the compressed-key form of the zero identity, the lowest of all.
var zeroKey = make([]byte, 33)

// identityKey returns the compressed public key of id, or zeroKey for the zero identity.
func identityKey(id *astral.Identity) []byte {
	if id.IsZero() {
		return zeroKey
	}
	return id.PublicKey().SerializeCompressed()
}

// dialedByLower reports whether link was dialed by the lower of its two identities, comparing compressed public keys byte-wise.
func dialedByLower(link *Link) bool {
	local, remote := identityKey(link.LocalIdentity()), identityKey(link.RemoteIdentity())
	if link.outbound {
		return bytes.Compare(local, remote) < 0
	}
	return bytes.Compare(remote, local) < 0
}

// outranks reports whether link a survives against link b under the duplicate rule, a being the one already in the pool.
// The duplicate rule: of two live links to one peer, the link the lower
// identity dialed survives, so the lower identity keeps its outbound link and
// the higher identity keeps the same link as inbound. Of two links one end
// dialed, the link already in the pool survives.
// why: both ends compute the same survivor from the identities alone, with no
// message exchange.
// note: only the end that dialed a losing link closes it, and only a prunable
// one; an inbound link is never closed by this rule.
func outranks(a, b *Link) bool {
	return dialedByLower(a) || !dialedByLower(b)
}

// offerStrategyLink settles a new outbound link a strategy produced.
// A watcher that asked for a new link keeps it. Otherwise the link stays
// unless a live link to the same peer outranks it, and then it closes with
// ErrExcessLink and the waiting callers get the survivor.
func (pool *LinkPool) offerStrategyLink(link *Link, strategy string) {
	checked := map[*Link]bool{}
	for {
		survivor, unchecked := pool.settleStrategyLink(link, &strategy, checked)
		if unchecked != nil {
			checked[unchecked] = confirmLive(unchecked)
			continue
		}
		if survivor == link {
			pool.notifyWatchers(pool.watchersKeeping(false), link, &strategy)
			return
		}
		pool.mod.log.Logv(1, "link %v with %v is excess; %v survives", link.id, link.RemoteIdentity(), survivor.id)
		link.CloseWithError(nodesmod.ErrExcessLink)
		pool.notifyWatchers(pool.watchersKeeping(false), survivor, nil)
		return
	}
}

// settleStrategyLink decides, under the pool lock, which link to the peer of link survives.
// It answers unchecked instead when the deciding link has not proven live and checked holds no verdict on it.
func (pool *LinkPool) settleStrategyLink(link *Link, strategy *string, checked map[*Link]bool) (survivor, unchecked *Link) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if pool.notifyWatchers(pool.watchersKeeping(true), link, strategy) {
		return link, nil
	}

	for _, other := range pool.links.Clone() {
		if other == link || other.Err() != nil || !other.RemoteIdentity().IsEqual(link.RemoteIdentity()) {
			continue
		}
		if !outranks(other, link) {
			continue
		}
		live, known := checked[other]
		switch {
		case live || other.heardRecently():
			return other, nil
		case !known:
			return nil, other
		}
	}

	// why: a link that later outranks this one finds it prunable and closes it (closeOutranked).
	link.prunable.Store(true)
	return link, nil
}

// closeOutranked closes with ErrExcessLink every prunable link to the peer of link that link outranks, once link proves live.
// why the proof: a peer that abandons its dial mid-negotiation leaves link
// registered here for a moment; closing an older link for it would leave none.
func (pool *LinkPool) closeOutranked(link *Link) {
	if len(pool.findOutranked(link)) == 0 || !confirmLive(link) {
		return
	}
	for _, l := range pool.findOutranked(link) {
		pool.mod.log.Logv(1, "link %v with %v is excess; %v survives", l.id, l.RemoteIdentity(), link.id)
		l.CloseWithError(nodesmod.ErrExcessLink)
	}
}

// findOutranked returns the prunable links to the peer of link that link outranks.
// why under the pool lock: a strategy link settled concurrently is either
// marked prunable before this scan or sees link in the pool.
func (pool *LinkPool) findOutranked(link *Link) []*Link {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	return pool.links.Select(func(other *Link) bool {
		return other != link &&
			other.prunable.Load() &&
			other.RemoteIdentity().IsEqual(link.RemoteIdentity()) &&
			!outranks(other, link)
	})
}

// heardRecently reports whether link received a frame from its peer since it was created and within its idle ping.
func (s *Link) heardRecently() bool {
	return s.lastRecv.Load() > s.createdAt.UnixNano() && s.idleLeft() > 0
}

// confirmLive reports whether the peer of link proves the link live: it was heard recently or answers a ping.
// note: while the keepalive's own ping is in flight, the answer waits for that ping's pong or timeout.
func confirmLive(link *Link) bool {
	if link.heardRecently() {
		return true
	}
	start := time.Now()
	_, err := link.Ping()
	if !errors.Is(err, errPingInFlight) {
		return err == nil
	}

	t := time.NewTimer(link.pingTimeout)
	defer t.Stop()
	select {
	case <-link.Done():
		return false
	case <-t.C:
		return link.Err() == nil && time.Unix(0, link.lastRecv.Load()).After(start)
	}
}
