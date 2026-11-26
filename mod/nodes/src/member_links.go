package nodes

import (
	"context"
	"sync"

	"github.com/astralp2p/astral-go/astral"
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
	// close replaces CloseLinks for the links of a member that left.
	// note: a test seam; nil in the running node.
	close func(*astral.Identity) error
}

// memberLink is the running link task of one member and the cancel that stops it.
type memberLink struct {
	task   *memberLinkTask
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
// mod/nodes learns its endpoints; a task whose member left is stopped, and
// the links to that member are closed.
//
// why a re-read: a member event is a signal to look, not a fact to apply, so
// events that arrive out of order still end in the latest set.
// why the early return: a failed read is no empty swarm, and stopping on it
// would close the link to every member.
func (mod *Module) reconcileMemberLinks() {
	left := mod.applyMemberSet()
	for _, id := range left {
		if err := mod.closeMemberLinks(id); err != nil {
			mod.log.Errorv(1, "closing links to former member %v: %v", id, err)
		}
	}
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

	members, err := mod.User.ReadLocalSwarm()
	if err != nil {
		mod.log.Errorv(1, "reading the swarm's members: %v", err)
		return nil
	}

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
		mod.log.Log("removing sibling %v", link.task.target)
		// why before the close: a task still running when its link closes redials it.
		link.cancel()
		left = append(left, link.task.target)
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
// registers it. The caller holds memberLinks.mu.
//
// why the endpoints: a membership reaches this node only through SwarmState
// sync, which brings no endpoints, so a new member's task would have none to
// dial; the pull's EndpointsUpdatedEvent wakes the task.
func (mod *Module) startMemberLink(member *astral.Identity) {
	task := newMemberLinkTask(member, mod, mod.log, mod.linkRetryClock)
	scheduled, err := mod.Scheduler.Schedule(task)
	if err != nil {
		mod.log.Error("error scheduling maintain link action: %v for node %v", err, member)
		return
	}

	if mod.memberLinks.tasks == nil {
		mod.memberLinks.tasks = map[string]memberLink{}
	}
	mod.log.Log("adding sibling %v", member)
	mod.memberLinks.tasks[member.String()] = memberLink{task: task, cancel: scheduled.Cancel}

	go mod.LearnMemberEndpoints(member)
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

// closeMemberLinks closes every link to id, a member that left.
func (mod *Module) closeMemberLinks(id *astral.Identity) error {
	if mod.memberLinks.close != nil {
		return mod.memberLinks.close(id)
	}
	return mod.CloseLinks(id)
}
