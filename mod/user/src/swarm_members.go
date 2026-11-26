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
	// expiry republishes the set at the earliest member expiry; nil when none is armed.
	expiry *time.Timer
}

// expiryMargin delays the republish past the expiry, so the auth index,
// which compares expires_at with the time of the read, already omits the member.
const expiryMargin = time.Second

// publishSwarm recomputes LocalSwarm and emits one SwarmMemberJoined or
// SwarmMemberLeft per node whose membership differs from the last published
// set. It emits nothing before the first set is recorded.
func (mod *Module) publishSwarm() {
	mod.memberSet.mu.Lock()
	defer mod.memberSet.mu.Unlock()
	if mod.memberSet.members == nil {
		return
	}
	next := identitySet(mod.LocalSwarm())
	for k, id := range next {
		if _, ok := mod.memberSet.members[k]; !ok {
			mod.Events.Emit(&user.SwarmMemberJoined{NodeID: id})
		}
	}
	for k, id := range mod.memberSet.members {
		if _, ok := next[k]; !ok {
			mod.Events.Emit(&user.SwarmMemberLeft{NodeID: id})
		}
	}
	mod.memberSet.members = next
	mod.armExpiry()
}

// recordSwarm records the current member set as published, without events.
func (mod *Module) recordSwarm() {
	mod.memberSet.mu.Lock()
	defer mod.memberSet.mu.Unlock()
	mod.memberSet.members = identitySet(mod.LocalSwarm())
	mod.armExpiry()
}

// armExpiry replaces the expiry timer with one that republishes the set at the
// earliest member expiry, and arms none when no member contract expires.
// The caller holds swarm.mu.
//
// why one stored timer: every publish re-arms, and unstopped timers would pile up.
func (mod *Module) armExpiry() {
	mod.stopExpiry()
	next := mod.nextMemberExpiry()
	if next.IsZero() {
		return
	}
	mod.memberSet.expiry = time.AfterFunc(next.Sub(mod.now())+expiryMargin, mod.publishSwarm)
}

// stopExpiry stops the expiry timer. The caller holds swarm.mu.
func (mod *Module) stopExpiry() {
	if mod.memberSet.expiry != nil {
		mod.memberSet.expiry.Stop()
		mod.memberSet.expiry = nil
	}
}

// nextMemberExpiry returns the earliest time at which a member other than this
// node leaves the swarm by expiry, or the zero time when none does.
//
// why the latest contract per subject: a member with an overlapping renewal
// stays in the swarm until its last contract expires.
// why not this node: a lapse of its own membership belongs to the active
// contract's lifecycle, not to a member change.
func (mod *Module) nextMemberExpiry() (next time.Time) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}
	last := map[string]time.Time{}
	for _, c := range mod.memberContracts() {
		if c.Subject.IsEqual(mod.node.Identity()) {
			continue
		}
		if t := c.ExpiresAt.Time(); t.After(last[c.Subject.String()]) {
			last[c.Subject.String()] = t
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

// memberContracts returns the membership that holds for each swarm member.
func (mod *Module) memberContracts() (contracts []*auth.SignedContract) {
	for _, id := range mod.swarmMembers() {
		if m, ok := mod.Membership(id); ok {
			contracts = append(contracts, m.SignedContract)
		}
	}
	return contracts
}

func identitySet(list []*astral.Identity) map[string]*astral.Identity {
	set := make(map[string]*astral.Identity, len(list))
	for _, id := range list {
		set[id.String()] = id
	}
	return set
}
