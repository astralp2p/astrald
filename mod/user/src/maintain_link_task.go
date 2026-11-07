package user

import (
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
	"github.com/astralp2p/astrald/mod/user"
)

var _ scheduler.EventReceiver = &MaintainLinkTask{}
var _ scheduler.Task = &MaintainLinkTask{}
var _ user.MaintainLinkTask = &MaintainLinkTask{}

// The backoff between failed link attempts: it starts at linkRetryMin, doubles
// after every failure up to linkRetryMax, and starts over after a success.
const (
	linkRetryMin = time.Second
	linkRetryMax = 15 * time.Minute
)

// MaintainLinkTask attempts to maintain a link to a target node indefinitely.
// triggers:
// - nodes.LinkClosedEvent (link to target dropped)
// - Wake (the user module saw target nearby)
type MaintainLinkTask struct {
	mod            *Module
	Target         *astral.Identity
	wake           chan struct{}
	actionRequired atomic.Bool
	// after is the backoff clock.
	after func(time.Duration) <-chan time.Time
}

func (mod *Module) NewMaintainLinkTask(target *astral.
	Identity) user.MaintainLinkTask {
	return &MaintainLinkTask{
		mod:    mod,
		Target: target,
		wake:   make(chan struct{}, 1),
		after:  time.After,
	}
}

func (a *MaintainLinkTask) String() string { return "nodes.maintain_link" }

func (a *MaintainLinkTask) Run(ctx *astral.Context) error {
	a.mod.log.Log("starting to maintain link to %v", a.Target)

	delay := linkRetryMin
	count := -1
	a.actionRequired.Store(!a.mod.Nodes.IsLinked(a.Target))
	for {
		if err := a.waitForAction(ctx); err != nil {
			return err
		}
		// why: cleared as the attempt starts, so a link close or a Wake during the attempt
		// sets it again and makes the loop dial once more instead of being lost.
		a.actionRequired.Store(false)

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
			count = max(count, 0) + 1
			a.actionRequired.Store(true)
			if err := a.backoff(ctx, delay); err != nil {
				return err
			}
			delay = min(2*delay, linkRetryMax)
			continue
		}

		delay = linkRetryMin
		if count > 0 {
			a.mod.log.Log("link to %v restored after %v attempts", a.Target,
				count)
		} else if count < 0 {
			a.mod.log.Log("link to %v established", a.Target)
		}

		count = 0
	}
}

// waitForAction returns once the link needs work, or with the error of ctx.
func (a *MaintainLinkTask) waitForAction(ctx *astral.Context) error {
	for !a.actionRequired.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.wake:
		}
	}
	return nil
}

// backoff waits out delay after a failed attempt; a wake ends the wait at once.
//
// why not sig.Retry: waking a pending sig.Retry wait and then calling Reset
// leaves the Retry running, so the next wait never ends (astral-go sig/retry.go).
func (a *MaintainLinkTask) backoff(ctx *astral.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.wake:
	case <-a.after(delay):
	}
	return nil
}

// Wake makes the task link to Target now when Target is not linked, also
// while the task waits out its backoff.
func (a *MaintainLinkTask) Wake() {
	if a.mod.Nodes.IsLinked(a.Target) {
		return
	}

	a.actionRequired.Store(true)
	a.signal()
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
			a.signal()
		}
	}
}

// signal wakes the loop; a wake already pending absorbs it.
func (a *MaintainLinkTask) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
