package nodes

import (
	"bytes"
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
func outranks(a, b *Link) bool {
	return dialedByLower(a) || !dialedByLower(b)
}

// atRisk reports whether a link the peer dials later can outrank link: link was dialed by the higher identity.
func atRisk(link *Link) bool {
	return !dialedByLower(link)
}

// offerStrategyLink settles a new outbound link a strategy produced and hands the survivor to the waiting callers.
// A watcher that asked for a new link keeps it, and nothing is pruned for it.
// Otherwise the link closes with ErrExcessLink when a live link to the same
// peer outranks it, and else it closes, with ErrExcessLink, the links it outranks.
// note: only this settlement prunes; a link arriving from the peer prunes
// nothing, so a link the peer forced never closes a link here.
func (pool *LinkPool) offerStrategyLink(link *Link, strategy string) {
	if pool.notifyWatchers(pool.watchersKeeping(true), link, &strategy) {
		return
	}
	// why the peer's negotiations: of two crossing dials, the first end to
	// settle waits out the rival's negotiation or the other end sees its
	// link, so both ends settle on one link.
	if !pool.awaitRivals(link, &pool.mod.inboundNegotiations) {
		return
	}

	checked := map[*Link]bool{}
	for {
		survivor, unchecked := pool.settleStrategyLink(link, checked)
		switch {
		case unchecked != nil:
			checked[unchecked] = confirmLive(unchecked)
			continue
		case survivor == link:
			pool.notifyWatchers(pool.watchersKeeping(false), link, &strategy)
		case survivor != nil:
			pool.notifyWatchers(pool.watchersKeeping(false), survivor, nil)
		}
		return
	}
}

// settleStrategyLink applies the duplicate rule to link under the pool lock and answers the survivor, nil when link is closed or refuted.
// It answers unchecked instead when a link the decision rests on has not proven live and checked holds no verdict on it.
// why under the lock: two links settled at once each see the other's outcome.
func (pool *LinkPool) settleStrategyLink(link *Link, checked map[*Link]bool) (survivor, unchecked *Link) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	if link.Err() != nil {
		return nil, nil
	}

	var losers []*Link
	for _, other := range pool.livePeerLinks(link) {
		if !outranks(other, link) {
			losers = append(losers, other)
			continue
		}
		live, known := checked[other]
		switch {
		case live || other.heardRecently():
			pool.closeExcess(link, other)
			return other, nil
		case !known:
			return nil, other
		}
	}

	if len(losers) > 0 {
		live, known := checked[link]
		switch {
		case !known && !link.heardRecently():
			return nil, link
		case known && !live:
			return nil, nil
		}
	}
	for _, l := range losers {
		pool.closeExcess(l, link)
	}
	return link, nil
}

// livePeerLinks returns the usable links to the peer of link, other than link.
// note: a link with an excess close pending is left out, as it was once closed outright.
func (pool *LinkPool) livePeerLinks(link *Link) []*Link {
	return pool.links.Select(func(other *Link) bool {
		return other != link && other.usable() && other.RemoteIdentity().IsEqual(link.RemoteIdentity())
	})
}

// closeExcess closes loser with ErrExcessLink in favor of survivor. When loser holds
// a migratable session, it first migrates its migratable sessions to survivor, in
// the background, and closes loser after; loser stays out of selection and
// settlement meanwhile.
// why the background: settleStrategyLink calls it under the pool lock, and a
// migration waits for the peer.
func (pool *LinkPool) closeExcess(loser, survivor *Link) {
	pool.mod.log.Logv(1, "link %v with %v is excess; %v survives", loser.id, loser.RemoteIdentity(), survivor.id)
	if !loser.excess.CompareAndSwap(false, true) {
		return
	}
	if !pool.mod.hasMigratable(loser) {
		loser.CloseWithError(nodesmod.ErrExcessLink)
		return
	}
	go func() {
		pool.mod.moveSessions(loser.id, survivor)
		loser.CloseWithError(nodesmod.ErrExcessLink)
	}()
}

// awaitRivals holds link back until the rival attempts with its peer that rivals counts end, at most its ping timeout, and an at-risk link also for one round trip on it first; it reports whether the link proved live.
// why the round trip: a rival still connecting when link arrived shows up
// in rivals within about one round trip.
func (pool *LinkPool) awaitRivals(link *Link, rivals *inFlight) bool {
	if atRisk(link) && !confirmLive(link) {
		return false
	}

	t := time.NewTimer(link.pingTimeout)
	defer t.Stop()
	select {
	case <-rivals.idle(link.RemoteIdentity()):
	case <-t.C:
	}
	return true
}

// deliverAtRisk hands the waiting callers the survivor among the links to the peer of an at-risk inbound link, once its rivals settled.
// why the own dials: a dial this node runs to the peer yields a link that
// outranks link, and the callers waiting on that dial must get it.
func (pool *LinkPool) deliverAtRisk(link *Link) {
	if !pool.awaitRivals(link, &pool.mod.dials) {
		return
	}
	if s := pool.findSurvivor(link.RemoteIdentity(), nil); s != nil {
		pool.notifyWatchers(pool.watchersKeeping(false), s, nil)
	}
}

// findSurvivor returns the open link to peer that the duplicate rule keeps among those accept takes (all when accept is nil), or nil.
func (pool *LinkPool) findSurvivor(peer *astral.Identity, accept func(*Link) bool) *Link {
	var best *Link
	for _, l := range pool.links.Clone() {
		if l.Err() != nil || !l.RemoteIdentity().IsEqual(peer) || (accept != nil && !accept(l)) {
			continue
		}
		if best == nil || keptOver(l, best) {
			best = l
		}
	}
	return best
}

// keptOver reports whether the duplicate rule keeps link a over link b regardless of pool order:
// the link the lower identity dialed, else the older link.
func keptOver(a, b *Link) bool {
	return dialedByLower(a) && !dialedByLower(b) ||
		dialedByLower(a) == dialedByLower(b) && a.createdAt.Before(b.createdAt)
}

// heardRecently reports whether link received a frame from its peer since it was created and within its idle ping.
func (s *Link) heardRecently() bool {
	return s.lastRecv.Load() > s.createdAt.UnixNano() && s.idleLeft() > 0
}

// confirmLive reports whether the peer of link proves the link live: it was heard recently or answers a ping.
// note: a ping already in flight, such as the keepalive's, is joined, not duplicated.
func confirmLive(link *Link) bool {
	if link.heardRecently() {
		return true
	}
	_, err := link.Ping()
	return err == nil
}
