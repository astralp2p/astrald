package nodes

import (
	"context"
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"slices"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
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
			mod.migrateSessions(e.LinkID, targetLink)
		} else {
			mod.log.Logv(2, "connectivity upgrade: no target link found, skipping migration for %v", e.RemoteIdentity)
		}

		select {
		case <-time.After(upgradeCooldown):
		case <-mod.ctx.Done():
		}
	})
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
