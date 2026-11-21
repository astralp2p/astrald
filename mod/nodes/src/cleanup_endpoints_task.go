package nodes

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
	"github.com/astralp2p/astrald/mod/scheduler"
)

var _ scheduler.Task = &CleanupEndpointsTask{}

type CleanupEndpointsTask struct {
	mod *Module
}

func (mod *Module) NewCleanupEndpointsTask() nodes.CleanupEndpointsTask {
	return &CleanupEndpointsTask{mod: mod}
}

func (c CleanupEndpointsTask) String() string {
	return "nodes.cleanup_endpoints"
}

func (c CleanupEndpointsTask) Run(ctx *astral.Context) error {
	deleted, err := c.mod.db.DeleteExpiredEndpoints(nodes.CleanupGrace)
	if err != nil {
		return err
	}

	if deleted > 0 {
		c.mod.log.Log("cleaned up %v expired endpoints", deleted)
	}

	// note: a health row outlives its endpoint row; only its own age removes it.
	stale, err := c.mod.db.DeleteStaleEndpointHealth(time.Now().Add(-endpointHealthRetention))
	if err != nil {
		return err
	}
	if stale > 0 {
		c.mod.log.Logv(1, "cleaned up %v stale endpoint health rows", stale)
	}

	delay, _ := c.mod.ctx.WithTimeout(nodes.CleanupInterval)
	_, err = c.mod.Scheduler.Schedule(c.mod.NewCleanupEndpointsTask(), delay)
	if err != nil {
		return err
	}

	return nil
}
