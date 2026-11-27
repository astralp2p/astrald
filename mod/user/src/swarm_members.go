package user

import (
	"sync"
	"time"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// memberSet is the local swarm member set the user module last published.
// nil before Run publishes the first set.
type memberSet struct {
	mu      sync.Mutex
	members map[string]*astral.Identity
	// banned holds the nodes publishBan emitted SwarmNodeBanned for.
	banned map[string]bool
	// expiry republishes the set at the earliest member expiry; nil when none is armed.
	expiry *time.Timer
}

// expiryMargin delays the republish past the expiry, so the auth index,
// which compares expires_at with the time of the read, already omits the member.
const expiryMargin = time.Second

// publishSwarm recomputes LocalSwarm and emits one SwarmMemberJoined or
// SwarmMemberLeft per node whose membership differs from the last published
// set. It emits nothing before the first set is recorded. mod/nodes maintains the members' links on
// these events.
func (mod *Module) publishSwarm() {
	mod.swarm.mu.Lock()
	defer mod.swarm.mu.Unlock()
	if mod.swarm.members == nil {
		return
	}
	next := identitySet(mod.LocalSwarm())
	for k, id := range next {
		if _, ok := mod.swarm.members[k]; !ok {
			mod.Events.Emit(&user.SwarmMemberJoined{NodeID: id})
		}
	}
	for k, id := range mod.swarm.members {
		if _, ok := next[k]; !ok {
			mod.Events.Emit(&user.SwarmMemberLeft{NodeID: id})
		}
	}
	mod.swarm.members = next
	mod.armExpiry()
}

// publishBan makes a stored ban on subject take effect: it reloads the ban
// set, republishes the member set, and emits SwarmNodeBanned for subject once.
// Like publishSwarm, it emits nothing before the first set is recorded.
// mod/nodes closes the links of a banned node on SwarmNodeBanned and on a
// member event, and on its first read.
//
// why for a non-member too: a banned node can be linked to this node without
// being in its member set (its membership has not reached SwarmState here, or
// it linked through user.introduce), and its links close as a member's do.
// Spec: .ai/system/protocols/user/types/mod.user.swarm_node_banned.md
func (mod *Module) publishBan(subject *astral.Identity) {
	mod.refreshBans()
	mod.publishSwarm()

	mod.swarm.mu.Lock()
	recorded := mod.swarm.members != nil
	first := recorded && !mod.swarm.banned[subject.String()]
	if first {
		mod.swarm.banned[subject.String()] = true
	}
	mod.swarm.mu.Unlock()

	if first {
		mod.Events.Emit(&user.SwarmNodeBanned{NodeID: subject})
	}
}

// recordSwarm records the current member set as published, without events.
func (mod *Module) recordSwarm() {
	mod.swarm.mu.Lock()
	defer mod.swarm.mu.Unlock()
	mod.swarm.members = identitySet(mod.LocalSwarm())
	if mod.swarm.banned == nil {
		mod.swarm.banned = map[string]bool{}
	}
	mod.armExpiry()
}

// armExpiry replaces the expiry timer with one that republishes the set at the
// earliest member expiry, and arms none when no member contract expires.
// The caller holds memberSet.mu.
//
// why: one timer is stored, because every publish re-arms and unstopped timers would pile up.
func (mod *Module) armExpiry() {
	mod.stopExpiry()
	next := mod.nextMemberExpiry()
	if next.IsZero() {
		return
	}
	mod.swarm.expiry = time.AfterFunc(next.Sub(mod.now())+expiryMargin, mod.publishSwarm)
}

// stopExpiry stops the expiry timer. The caller holds memberSet.mu.
func (mod *Module) stopExpiry() {
	if mod.swarm.expiry != nil {
		mod.swarm.expiry.Stop()
		mod.swarm.expiry = nil
	}
}

// nextMemberExpiry returns the earliest time at which a member other than this
// node leaves the swarm by expiry, or the zero time when none does.
//
// why: the latest contract per subject counts, because a member with an
// overlapping renewal stays in the swarm until its last contract expires; this
// node is left out, because a lapse of its own membership belongs to the active
// contract's lifecycle, not to a member change.
func (mod *Module) nextMemberExpiry() (next time.Time) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}
	last := map[string]time.Time{}
	for _, id := range mod.snapshot().Members() {
		m, ok := mod.Membership(id)
		if !ok || id.IsEqual(mod.node.Identity()) {
			continue
		}
		if t := m.SignedContract.ExpiresAt.Time(); t.After(last[id.String()]) {
			last[id.String()] = t
		}
	}
	for _, t := range last {
		if next.IsZero() || t.Before(next) {
			next = t
		}
	}
	return
}

// onContractIndexed republishes the member set after a membership enters
// the auth index, on any path that indexes it.
func (mod *Module) onContractIndexed(signed *auth.SignedContract) {
	if _, ok := user.ContractTier(signed.Contract); ok {
		mod.publishSwarm()
	}
}

func identitySet(list []*astral.Identity) map[string]*astral.Identity {
	set := make(map[string]*astral.Identity, len(list))
	for _, id := range list {
		set[id.String()] = id
	}
	return set
}
