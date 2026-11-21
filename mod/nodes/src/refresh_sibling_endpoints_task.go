package nodes

import (
	"math/rand/v2"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

// Endpoint exchange in the local swarm: .ai/system/protocols/nodes/README.md#endpoint-exchange-in-the-local-swarm
const (
	// defaultSiblingRefreshInterval is the period of the re-pull of every linked sibling.
	// why: a sibling's public tcp and kcp rows expire after 7 days, so one pull
	// per day keeps them alive while the link stays up.
	defaultSiblingRefreshInterval = 24 * time.Hour
	// defaultLocalRefreshInterval is the period of the task, and of the re-pull
	// of a linked sibling that has an unexpired local row.
	// why: well inside ip.LocalEndpointTTL (2 h), so a local row is renewed
	// several times before it could lapse while the link stays up.
	defaultLocalRefreshInterval = 35 * time.Minute
	// siblingRefreshJitter is the share of the period by which a run is moved
	// earlier or later.
	siblingRefreshJitter = 0.10
)

var _ scheduler.Task = &RefreshSiblingEndpointsTask{}

// RefreshSiblingEndpointsTask re-pulls the endpoints of linked local swarm
// siblings and then checks whether this node's own endpoint set changed. It
// runs every localRefreshInterval: each run pulls the siblings that have an
// unexpired local row, and a run once every siblingRefreshInterval pulls every
// linked sibling. It schedules its next run once the pulls are started.
// why: a row is renewed only by a pull, and a link that stays up for days gets
// no event that pulls; a set that changed without an event, such as an onion
// that appeared, reaches the siblings through the announcement check.
// why no pull without a link: a local row of a network the sibling left then
// lapses within ip.LocalEndpointTTL.
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

// Run starts one pull through syncGate for every linked sibling the run
// covers, requests the announcement check and schedules the next run.
// note: the pulls run apart from the task, bounded by endpointsPullTimeout each,
// so a sibling that never answers delays neither the others nor the next run.
func (t *RefreshSiblingEndpointsTask) Run(*astral.Context) error {
	mod := t.mod
	self := mod.node.Identity()
	full := mod.takeFullRefresh(mod.refreshNow())

	for _, id := range mod.User.LocalSwarm() {
		// why: the pull uses an existing link and never dials, so an unlinked
		// sibling costs nothing and has no source to pull from.
		if id.IsEqual(self) || !mod.IsLinked(id) {
			continue
		}
		if !full && !mod.hasLocalEndpoints(id) {
			continue
		}

		mod.syncGate.Run(id, func() { mod.pullSiblingEndpoints(id) })
	}

	mod.scheduleEndpointsAnnounce()

	delay, _ := mod.ctx.WithTimeout(jitter(mod.localRefreshInterval, siblingRefreshJitter))
	_, err := mod.Scheduler.Schedule(mod.newRefreshSiblingEndpointsTask(), delay)
	return err
}

// takeFullRefresh reports whether a run at now pulls every linked sibling.
// It moves the next full run one jittered siblingRefreshInterval past now when
// it does. A zero due time is due.
func (mod *Module) takeFullRefresh(now time.Time) bool {
	if due := mod.fullRefreshDue.Load(); due != 0 && now.UnixNano() < due {
		return false
	}
	mod.deferFullRefresh(now)
	return true
}

// deferFullRefresh makes the next full run due one jittered siblingRefreshInterval after now.
func (mod *Module) deferFullRefresh(now time.Time) {
	mod.fullRefreshDue.Store(now.Add(jitter(mod.siblingRefreshInterval, siblingRefreshJitter)).UnixNano())
}

// refreshNow returns the time of the refresh task's clock.
func (mod *Module) refreshNow() time.Time {
	if mod.refreshClock == nil {
		return time.Now()
	}
	return mod.refreshClock.Now()
}

// hasLocalEndpoints reports whether id has an unexpired tcp or kcp row on a non-public IP.
func (mod *Module) hasLocalEndpoints(id *astral.Identity) bool {
	rows, err := mod.db.FindEndpoints(id)
	if err != nil {
		mod.log.Error("finding endpoints of %v: %v", id, err)
		return false
	}
	return slices.ContainsFunc(rows, func(row *dbEndpoint) bool {
		return isLocalEndpoint(row.Network, row.Address)
	})
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
