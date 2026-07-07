package user

import (
	nodesmod "github.com/cryptopunkscc/astrald/mod/nodes"
	"sync/atomic"
	"time"

	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/sig"
	"github.com/cryptopunkscc/astrald/mod/events"
	"github.com/cryptopunkscc/astrald/mod/scheduler"
	"github.com/cryptopunkscc/astrald/mod/user"
)

var _ scheduler.EventReceiver = &MaintainLinkTask{}
var _ scheduler.Task = &MaintainLinkTask{}

// MaintainLinkTask attempts to maintain a link to a target node indefinitely.
// triggers:
// - nodes.LinkClosedEvent (link to target dropped)
type MaintainLinkTask struct {
	mod            *Module
	Target         *astral.Identity
	wake           chan struct{}
	actionRequired atomic.Bool
}

func (mod *Module) NewMaintainLinkTask(target *astral.
	Identity) user.MaintainLinkTask {
	return &MaintainLinkTask{
		mod:    mod,
		Target: target,
		wake:   make(chan struct{}, 1),
	}
}

func (a *MaintainLinkTask) String() string { return "nodes.maintain_link" }

func (a *MaintainLinkTask) Run(ctx *astral.Context) error {
	a.mod.log.Log("starting to maintain link to %v", a.Target)
	retry, err := sig.NewRetry(time.Second, 15*time.Minute, 2)
	if err != nil {
		return err
	}

	count := -1
	a.actionRequired.Store(!a.mod.Nodes.IsLinked(a.Target))
	for {
		for !a.actionRequired.Load() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-a.wake:
			}
		}

		switch {
		case count == 0:
			a.mod.log.Log("link to %v is broken, trying to reconnect", a.Target)
		case count > 0 && count%5 == 0:
			a.mod.log.Log("still trying to reconnect to %v (attempt %v)", a.Target, count)
		}

		task := a.mod.Nodes.NewEnsureLinkTask(a.Target, []string{nodesmod.StrategyBasic, nodesmod.StrategyTor}, nil, false)
		scheduled, err := a.mod.Scheduler.Schedule(task)
		if err != nil {
			return err
		}

		<-scheduled.Done()
		if scheduled.Err() != nil {
			count = <-retry.Retry()
			if count == 0 && a.actionRequired.Load() {
				count = 1
			}

			continue
		}

		retry.Reset()
		if count > 0 {
			a.mod.log.Log("link to %v restored after %v attempts", a.Target,
				count)
		} else if count < 0 {
			a.mod.log.Log("link to %v established", a.Target)
		}

		count = 0
		a.actionRequired.Store(false)
	}
}

// ReceiveEvent wakes the reconnect loop when the last link to Target is closed.
// Ignores events for other identities and partial disconnects (LinkCount != 0).
func (a *MaintainLinkTask) ReceiveEvent(e *events.Event) {
	switch typed := e.Data.(type) {
	case *nodes.LinkClosedEvent:
		if !typed.RemoteIdentity.IsEqual(a.Target) || typed.LinkCount != 0 {
			return
		}

		if !a.actionRequired.Swap(true) {
			select {
			case a.wake <- struct{}{}:
			default:
			}
		}
	}
}
