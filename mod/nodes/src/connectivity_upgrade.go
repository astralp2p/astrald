package nodes

import (
	"context"
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"slices"
	"time"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/sig"
)

const upgradeTimeout = 3 * time.Minute
const upgradeCooldown = 5 * time.Minute

func (mod *Module) connectivityUpgrade(e *nodes.LinkPressureEvent) {
	connectivityGate := &sig.Switch{}
	if existing, ok := mod.upgraders.Set(e.RemoteIdentity.String(), connectivityGate); !ok {
		connectivityGate = existing
	}

	connectivityGate.Run(mod.ctx, func(_ context.Context) {
		mod.log.Log("connectivity upgrade triggered for %v (link %v)", e.RemoteIdentity, e.LinkID)

		var targetLink *Link
		alternatives := mod.linkPool.links.Select(func(link *Link) bool {
			return link.RemoteIdentity().IsEqual(e.RemoteIdentity) && link.id != e.LinkID
		})

		slices.SortFunc(alternatives, func(a, b *Link) int {
			if (a.pressure == nil) == (b.pressure == nil) {
				return 0
			}
			if a.pressure == nil {
				return -1
			}

			return 1
		})

		if len(alternatives) > 0 {
			targetLink = alternatives[0]
			mod.log.Log("connectivity upgrade: reusing existing link %v (%v)", targetLink.id, targetLink.Network())
		} else {
			ctx, cancel := mod.ctx.WithTimeout(upgradeTimeout)
			defer cancel()

			mod.log.Log("connectivity upgrade: starting NAT traversal with %v", e.RemoteIdentity)
			result := <-mod.linkPool.RetrieveLink(ctx, e.RemoteIdentity,
				WithForceNew(), WithStrategies(nodesmod.StrategyNAT))

			if result.Err != nil {
				mod.log.Log("connectivity upgrade with %v failed: %v", e.RemoteIdentity, result.Err)
			} else {
				targetLink = result.Link
				mod.log.Log("connectivity upgrade: established new link %v (%v)", targetLink.id, targetLink.Network())
			}
		}

		if targetLink != nil {
			mod.moveSessions(e.LinkID, targetLink)
			mod.retireAfterUpgrade(e.LinkID, targetLink)
		} else {
			mod.log.Logv(2, "connectivity upgrade: no target link found, skipping migration for %v", e.RemoteIdentity)
		}

		select {
		case <-time.After(upgradeCooldown):
		case <-mod.ctx.Done():
		}
	})
}

// retireMax is the longest retireLink waits for the sessions of a link to close.
// why 2 min: a session younger than minSessionAge (30 s) at the upgrade becomes migratable within it.
// note: a session still open and not migratable at this bound is cut.
const retireMax = 2 * time.Minute

// retirePoll is the interval at which retireLink checks a retiring link for open sessions.
const retirePoll = time.Second

// retireAfterUpgrade starts retireLink on the link oldID in favor of target when target has the higher transport rank, and reports whether it started.
// why only a higher rank: an upgrade to an equal or lower rank gains no speed, so both links stay (U3).
func (mod *Module) retireAfterUpgrade(oldID astral.Nonce, target *Link) bool {
	old := mod.getLinkByID(oldID)
	if old == nil || old == target || rankOf(target.Network()) <= rankOf(old.Network()) {
		return false
	}
	mod.log.Logv(1, "retiring %v link %v with %v after the upgrade to %v link %v",
		old.Network(), old.id, old.RemoteIdentity(), target.Network(), target.id)
	go mod.retireLink(old, target)
	return true
}

// retireLink closes old with ErrLinkRetired once it holds no session or retireMax passed, whichever comes first.
// Before the close it migrates to target the sessions that became migratable meanwhile.
// It leaves old open when old or target closes first or the module stops.
func (mod *Module) retireLink(old, target *Link) {
	clock := mod.retireClock
	if clock == nil {
		clock = realClock{}
	}
	deadline := clock.Now().Add(retireMax)
	for old.GetMux().sessions.Len() > 0 {
		left := deadline.Sub(clock.Now())
		if left <= 0 {
			break
		}
		select {
		case <-clock.After(min(retirePoll, left)):
		case <-old.Done():
			return
		case <-target.Done():
			return
		case <-mod.ctx.Done():
			return
		}
	}
	if old.Err() != nil || target.Err() != nil {
		return
	}

	mod.moveSessions(old.id, target)
	mod.log.Log("retired %v link %v with %v; %v link %v carries its traffic",
		old.Network(), old.id, old.RemoteIdentity(), target.Network(), target.id)
	old.CloseWithError(nodesmod.ErrLinkRetired)
}

// moveSessions migrates the migratable sessions of the link oldLinkID to newLink through mod.migrate, else migrateSessions.
func (mod *Module) moveSessions(oldLinkID astral.Nonce, newLink *Link) {
	if mod.migrate != nil {
		mod.migrate(oldLinkID, newLink)
		return
	}
	mod.migrateSessions(oldLinkID, newLink)
}

// hasMigratable reports whether link holds an open session that canAutoMigrate accepts.
func (mod *Module) hasMigratable(link *Link) bool {
	return len(link.GetMux().sessions.Select(func(_ astral.Nonce, v *session) bool {
		return v.IsOpen() && mod.canAutoMigrate(v)
	})) > 0
}

const migrateSessionTimeout = 30 * time.Second

func (mod *Module) migrateSessions(oldLinkID astral.Nonce, newLink *Link) {
	oldLink := mod.getLinkByID(oldLinkID)
	if oldLink == nil {
		mod.log.Logv(1, "migrate sessions: old link %v not found", oldLinkID)
		return
	}

	var sessions map[astral.Nonce]*session
	if oldLink != nil {
		sessions = oldLink.GetMux().sessions.Select(func(k astral.Nonce, v *session) bool {
			return v.IsOpen() && mod.canAutoMigrate(v)
		})
	}

	if len(sessions) == 0 {
		return
	}

	mod.log.Log("migrating %v sessions from link %v to %v", len(sessions), oldLinkID, newLink.id)

	var migrated int
	for _, sess := range sessions {
		ctx, cancel := mod.ctx.WithTimeout(migrateSessionTimeout)
		err := mod.migrateSession(ctx, sess, newLink)
		cancel()

		if err != nil {
			mod.log.Logv(1, "migrate session %v failed: %v", sess.Nonce, err)
			continue
		}
		migrated++
	}

	mod.log.Log("migrated %v/%v sessions from link %v to %v", migrated, len(sessions), oldLinkID, newLink.id)
}

func (mod *Module) canAutoMigrate(s *session) bool {
	return time.Since(s.createdAt) >= minSessionAge || s.bytes.Load() >= minSessionBytes
}
