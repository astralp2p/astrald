package nodes

import (
	"context"
	"sync"

	"github.com/astralp2p/astral-go/api/nodes"
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
// the links to that member are closed, unless the user banned it. It never
// closes the links of a banned node otherwise.
//
// why: a member event is a signal to look, not a fact to apply, so events that
// arrive out of order still end in the latest set.
// why not a banned node's links: until the banned node answers its ban, the
// links carry the ban to it (Expel's push, the redelivery at a new link, the
// sweep), and the spec admits them until then. They close when it answers
// (onBanAnswered, EnforceLinkPolicy) or at the sweep.
func (mod *Module) reconcileMemberLinks() {
	for _, id := range mod.applyMemberSet() {
		if err := mod.CloseLinks(id); err != nil {
			mod.log.Errorv(1, "closing links to %v: %v", id, err)
		}
	}
}

// applyMemberSet starts and stops link tasks against the current member set,
// and returns the nodes whose links to close: the members whose tasks it
// stopped, except the banned ones, whose links wait for the ban answer.
func (mod *Module) applyMemberSet() (closing []*astral.Identity) {
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
		// why: a task still running when its link closes redials it, so it is cancelled first.
		link.cancel()
		if !mod.User.IsBanned(link.target) {
			closing = append(closing, link.target)
		}
	}

	for key, m := range keep {
		if _, ok := ml.tasks[key]; ok {
			continue
		}
		mod.startMemberLink(m)
	}

	return closing
}

// startMemberLink schedules the link task of a member that has none and
// registers it, and learns the member's endpoints. The caller holds memberLinks.mu.
//
// why the endpoints: a membership reaches this node only through SwarmState
// sync, which brings no endpoints, so a new member's task would have none to
// dial. A linked member is pulled from itself. Otherwise the linked members
// are pulled one at a time until a pull stores a new row, which then wakes the
// task through the EndpointsUpdatedEvent.
func (mod *Module) startMemberLink(member *astral.Identity) {
	task := mod.NewMaintainLinkTask(member)
	scheduled, err := mod.Scheduler.Schedule(task)
	if err != nil {
		mod.log.Error("member link task: schedule link task for %v: %v", member, err)
		return
	}

	if mod.memberLinks.tasks == nil {
		mod.memberLinks.tasks = map[string]memberLink{}
	}
	mod.log.Log("adding sibling %v", member)
	mod.memberLinks.tasks[member.String()] = memberLink{target: member, task: task, cancel: scheduled.Cancel}

	go func() {
		self := mod.node.Identity()
		if mod.IsLinked(member) {
			mod.RefreshMemberEndpoints(member)
			return
		}

		for _, resolver := range mod.User.LocalSwarm() {
			if resolver.IsEqual(self) || resolver.IsEqual(member) || !mod.IsLinked(resolver) {
				continue
			}

			ctx, cancel := mod.ctx.WithTimeout(endpointsPullTimeout)
			added, err := mod.UpdateNodeEndpoints(ctx, resolver, member)
			cancel()
			if err != nil {
				mod.log.Logv(1, "pulling endpoints of new member %v from %v: %v", member, resolver, err)
				continue
			}
			if added > 0 {
				mod.log.Logv(1, "pulled %v new endpoints of new member %v from %v", added, member, resolver)
				mod.linkPool.endpointsAdded(member)
				mod.Events.Emit(&nodes.EndpointsUpdatedEvent{Identity: member})
				return
			}
		}
	}()
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
