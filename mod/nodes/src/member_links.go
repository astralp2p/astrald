package nodes

import (
	"context"
	"sync"

	"github.com/astralp2p/astral-go/astral"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
)

// memberLinks holds one link task for every swarm member other than this
// node, keyed by the member's identity.
type memberLinks struct {
	// mu makes reading the member set, starting and stopping tasks one step.
	// why: member events, nearby sightings and the first read reconcile at
	// once, and two unserialized runs each start a task for one member.
	mu sync.Mutex
	// started is set once Run read the first member set; reconcile does
	// nothing before it.
	started bool
	tasks   map[string]memberLink
	// banned holds the banned nodes whose links were closed, so a ban closes
	// a node's links once.
	// why once: AdmitLink admits a banned node's later links until it answers
	// the push of its ban, and those links carry the ban to it.
	banned map[string]struct{}
}

// memberLink is the running link task of one member and the cancel that stops it.
type memberLink struct {
	target *astral.Identity
	task   nodesmod.MaintainLinkTask
	cancel context.CancelFunc
}

// runMemberLinks starts the link tasks of the swarm's members once the user
// module applied its first member set. Swarm member events reconcile them
// afterwards (ReceiveObject).
func (mod *Module) runMemberLinks(ctx *astral.Context) {
	select {
	case <-mod.User.Ready():
	case <-ctx.Done():
		return
	}

	mod.memberLinks.mu.Lock()
	mod.memberLinks.started = true
	mod.memberLinks.mu.Unlock()

	mod.reconcileMemberLinks()
}

// reconcileMemberLinks re-reads the swarm's member set and makes the link
// tasks match it: a member other than this node without a task gets one, and
// mod/nodes refreshes its endpoints; a task whose member left is stopped, and
// the links to that member are closed. The links of every linked node the
// user banned since the last reconcile are closed too, member or not.
//
// why a re-read: a member event is a signal to look, not a fact to apply, so
// events that arrive out of order still end in the latest set.
// why the early return: a failed read is no empty swarm, and stopping on it
// would close the link to every member.
func (mod *Module) reconcileMemberLinks() {
	closing := identitySet(mod.applyMemberSet())
	for k, id := range identitySet(mod.findNewBans()) {
		closing[k] = id
	}
	for _, id := range closing {
		if err := mod.CloseLinks(id); err != nil {
			mod.log.Errorv(1, "closing links to %v: %v", id, err)
		}
	}
}

// findNewBans returns the linked nodes the user banned whose links no
// reconcile closed yet, and records them as closed.
//
// why every linked node: a banned node can be linked without being a member
// here (its membership has not reached SwarmState, or it linked through
// user.introduce), so it has no link task to stop.
func (mod *Module) findNewBans() (banned []*astral.Identity) {
	ml := &mod.memberLinks
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if !ml.started {
		return nil
	}

	for _, id := range mod.LinkedIdentities() {
		if _, done := ml.banned[id.String()]; done || !mod.User.IsBanned(id) {
			continue
		}
		if ml.banned == nil {
			ml.banned = map[string]struct{}{}
		}
		ml.banned[id.String()] = struct{}{}
		banned = append(banned, id)
	}
	return banned
}

// applyMemberSet starts and stops link tasks against the current member set,
// and returns the members whose tasks it stopped.
func (mod *Module) applyMemberSet() (left []*astral.Identity) {
	ml := &mod.memberLinks
	ml.mu.Lock()
	defer ml.mu.Unlock()
	if !ml.started {
		return nil
	}

	members := mod.User.LocalSwarm()
	keep := make(map[string]*astral.Identity, len(members))
	for _, m := range members {
		if !m.IsZero() && !m.IsEqual(mod.node.Identity()) {
			keep[m.String()] = m
		}
	}

	for key, link := range ml.tasks {
		if _, ok := keep[key]; ok {
			continue
		}
		delete(ml.tasks, key)
		mod.log.Log("removing sibling %v", link.target)
		// why before the close: a task still running when its link closes redials it.
		link.cancel()
		left = append(left, link.target)
	}

	for key, m := range keep {
		if _, ok := ml.tasks[key]; ok {
			continue
		}
		mod.startMemberLink(m)
	}
	return left
}

// startMemberLink schedules the link task of a member that has none and
// registers it, and refreshes the member's endpoints. The caller holds memberLinks.mu.
func (mod *Module) startMemberLink(member *astral.Identity) {
	task := mod.NewMaintainLinkTask(member)
	scheduled, err := mod.Scheduler.Schedule(task)
	if err != nil {
		mod.log.Error("error scheduling maintain link action: %v for node %v", err, member)
		return
	}

	if mod.memberLinks.tasks == nil {
		mod.memberLinks.tasks = map[string]memberLink{}
	}
	mod.log.Log("adding sibling %v", member)
	mod.memberLinks.tasks[member.String()] = memberLink{target: member, task: task, cancel: scheduled.Cancel}

	go mod.RefreshMemberEndpoints(member)
}

// wakeMemberLink makes the link task of id link now. It reports false when id
// has no link task.
func (mod *Module) wakeMemberLink(id *astral.Identity) bool {
	mod.memberLinks.mu.Lock()
	link, ok := mod.memberLinks.tasks[id.String()]
	mod.memberLinks.mu.Unlock()
	if !ok {
		return false
	}

	link.task.Wake()
	return true
}

// onMemberSeen makes the link task of a swarm member seen nearby link now,
// unless the member is linked. A member without a link task gets one, and a
// new task links at once.
func (mod *Module) onMemberSeen(id *astral.Identity) {
	if id.IsZero() || mod.IsLinked(id) || !mod.User.IsSwarmMember(id) {
		return
	}

	mod.log.Log("sibling %v is nearby, linking now", id)
	if !mod.wakeMemberLink(id) {
		mod.reconcileMemberLinks()
	}
}

func identitySet(list []*astral.Identity) map[string]*astral.Identity {
	set := make(map[string]*astral.Identity, len(list))
	for _, id := range list {
		set[id.String()] = id
	}
	return set
}
