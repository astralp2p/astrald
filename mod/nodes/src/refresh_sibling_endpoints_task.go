package nodes

import (
	"math/rand/v2"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// Endpoint exchange in the local swarm: .ai/system/protocols/nodes/README.md#endpoint-exchange-in-the-local-swarm
const (
	// defaultSiblingRefreshInterval is the period of the sibling endpoint re-pull.
	// why: a sibling's tcp and kcp rows expire after 7 days, so one pull per day
	// keeps them alive while the link stays up.
	defaultSiblingRefreshInterval = 24 * time.Hour
	// siblingRefreshJitter is the share of the period by which a run is moved
	// earlier or later.
	siblingRefreshJitter = 0.10
)

var _ scheduler.Task = &RefreshSiblingEndpointsTask{}

// RefreshSiblingEndpointsTask re-pulls the endpoints of every linked local swarm
// sibling and then checks whether this node's own endpoint set changed. It
// schedules its next run once the pulls are started.
// why: a row is renewed only by a pull, and a link that stays up for days gets
// no event that pulls; a set that changed without an event, such as an onion
// that appeared, reaches the siblings through the announcement check.
// note: the task is a removable layer. Deleting this file and the Schedule call
// in Module.Run removes it; pullSiblingEndpoints stays, used by onPartialLinkClose.
type RefreshSiblingEndpointsTask struct {
	mod *Module
}

func (mod *Module) newRefreshSiblingEndpointsTask() *RefreshSiblingEndpointsTask {
	return &RefreshSiblingEndpointsTask{mod: mod}
}

func (t *RefreshSiblingEndpointsTask) String() string {
	return "nodes.refresh_sibling_endpoints"
}

// Run starts one pull for every linked sibling through syncGate, requests the
// announcement check and schedules the next run.
// note: the pulls run apart from the task, bounded by endpointsPullTimeout each,
// so a sibling that never answers delays neither the others nor the next run.
func (t *RefreshSiblingEndpointsTask) Run(*astral.Context) error {
	mod := t.mod
	self := mod.node.Identity()

	for _, id := range mod.User.LocalSwarm() {
		// why: the pull uses an existing link and never dials, so an unlinked
		// sibling costs nothing and has no source to pull from.
		if id.IsEqual(self) || !mod.IsLinked(id) {
			continue
		}

		mod.syncGate.Run(id, func() { mod.pullSiblingEndpoints(id) })
	}

	mod.scheduleEndpointsAnnounce()

	delay, _ := mod.ctx.WithTimeout(jitter(mod.siblingRefreshInterval, siblingRefreshJitter))
	_, err := mod.Scheduler.Schedule(mod.newRefreshSiblingEndpointsTask(), delay)
	return err
}

// pullSiblingEndpoints pulls the endpoints of sibling id from id over an
// existing link, bounded by endpointsPullTimeout.
// note: every syncGate run for a sibling passes this function, as the gate
// reruns the function of the call that started the run.
func (mod *Module) pullSiblingEndpoints(id *astral.Identity) {
	ctx, cancel := mod.ctx.WithTimeout(endpointsPullTimeout)
	defer cancel()

	if _, err := mod.UpdateNodeEndpoints(ctx, id, id); err != nil {
		mod.log.Error("pulling endpoints of sibling %v: %v", id, err)
	}
}

// jitter returns d moved by a uniformly random share of at most frac of d, in
// either direction.
func jitter(d time.Duration, frac float64) time.Duration {
	return d + time.Duration((rand.Float64()*2-1)*frac*float64(d))
}
