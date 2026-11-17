package user

import (
	"sync"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
)

// memberSet is the local swarm member set the user module last published.
// nil before Run publishes the first set.
type memberSet struct {
	mu      sync.Mutex
	members map[string]*astral.Identity
}

// publishSwarm recomputes LocalSwarm and emits one SwarmMemberJoined or
// SwarmMemberLeft per node whose membership differs from the last published
// set. It emits nothing before the first set is recorded.
// fixme: a membership contract that expires changes LocalSwarm without
// emitting SwarmMemberLeft; nothing republishes the set at expiry.
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
}

// recordSwarm records the current member set as published, without events.
func (mod *Module) recordSwarm() {
	mod.swarm.mu.Lock()
	defer mod.swarm.mu.Unlock()
	mod.swarm.members = identitySet(mod.LocalSwarm())
}

// onContractIndexed republishes the member set after a node contract enters
// the auth index, on any path that indexes it.
func (mod *Module) onContractIndexed(signed *auth.SignedContract) {
	if user.IsNodeContract(signed.Contract) {
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
