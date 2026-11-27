package nodes

import (
	nodesmod "github.com/astralp2p/astrald/mod/nodes"
	"slices"
	"sync/atomic"
	"time"

	"github.com/astralp2p/astral-go/api/events"
	"github.com/astralp2p/astral-go/api/ip"
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/scheduler"
)

var _ scheduler.EventReceiver = &MaintainLinkTask{}
var _ scheduler.Task = &MaintainLinkTask{}
var _ nodesmod.MaintainLinkTask = &MaintainLinkTask{}

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
// - ip.EventNetworkAddressChanged that adds a routable address (also restarts the backoff)
type MaintainLinkTask struct {
	mod            *Module
	Target         *astral.Identity
	wake           chan struct{}
	actionRequired atomic.Bool
	// resetBackoff makes the next attempt start the backoff over from linkRetryMin.
	resetBackoff atomic.Bool
	// after is the backoff clock.
	after func(time.Duration) <-chan time.Time
}

func (mod *Module) NewMaintainLinkTask(target *astral.
	Identity) nodesmod.MaintainLinkTask {
	return &MaintainLinkTask{
		mod:    mod,
		Target: target,
		wake:   make(chan struct{}, 1),
		after:  mod.linkRetryClock,
	}
}

// SetLinkRetryClock makes after time the backoff of every MaintainLinkTask, running ones included.
// A clock whose channel never fires leaves Wake, a link close and an address change as the only retries.
// note: a test seam; an in-process node test reaches it through the module interface.
func (mod *Module) SetLinkRetryClock(after func(time.Duration) <-chan time.Time) {
	mod.linkRetryAfter.Store(&after)
}

// linkRetryClock waits d on the clock SetLinkRetryClock set, or on time.After.
func (mod *Module) linkRetryClock(d time.Duration) <-chan time.Time {
	if after := mod.linkRetryAfter.Load(); after != nil {
		return (*after)(d)
	}
	return time.After(d)
}

func (a *MaintainLinkTask) String() string { return "nodes.maintain_link" }

func (a *MaintainLinkTask) Run(ctx *astral.Context) error {
	a.mod.log.Log("starting to maintain link to %v", a.Target)

	delay := linkRetryMin
	count := -1
	a.actionRequired.Store(!a.mod.IsLinked(a.Target))
	for {
		if err := a.waitForAction(ctx); err != nil {
			return err
		}
		// why: cleared as the attempt starts, so a link close or a Wake during the attempt
		// sets it again and makes the loop dial once more instead of being lost.
		a.actionRequired.Store(false)
		if a.resetBackoff.Swap(false) {
			delay = linkRetryMin
		}

		switch {
		// why: a Wake during the previous, successful attempt also lands here with
		// count == 0; the link then stands and is not broken.
		case count == 0 && !a.mod.IsLinked(a.Target):
			a.mod.log.Log("link to %v is broken, trying to reconnect", a.Target)
		case count > 0 && count%5 == 0:
			a.mod.log.Log("still trying to reconnect to %v (attempt %v)", a.Target, count)
		}

		task := a.mod.NewEnsureLinkTask(a.Target, []string{nodesmod.StrategyBasic, nodesmod.StrategyTor}, nil, false)
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
// why: ctx is checked first because a link close or Wake racing the cancel sets
// actionRequired and fills wake, and either would otherwise start one more dial.
func (a *MaintainLinkTask) waitForAction(ctx *astral.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if a.mod.IsLinked(a.Target) {
		return
	}

	a.actionRequired.Store(true)
	a.signal()
}

// ReceiveEvent wakes the reconnect loop when the last link to Target is closed.
// Ignores events for other identities and partial disconnects (LinkCount != 0).
// A local address change wakes an unlinked task and restarts its backoff, so a
// node that regains network dials without waiting out a backoff of up to
// linkRetryMax.
func (a *MaintainLinkTask) ReceiveEvent(e *events.Event) {
	switch typed := e.Data.(type) {
	case *ip.EventNetworkAddressChanged:
		if !addsRoutableAddress(typed) || a.mod.IsLinked(a.Target) {
			return
		}
		a.resetBackoff.Store(true)
		a.actionRequired.Store(true)
		a.signal()

	case *nodes.LinkClosedEvent:
		if !typed.RemoteIdentity.IsEqual(a.Target) || typed.LinkCount != 0 {
			return
		}

		if !a.actionRequired.Swap(true) {
			a.signal()
		}
	}
}

// addsRoutableAddress reports whether e adds a global unicast address.
//
// why: a removed address makes no failed dial succeed, and a link-local address
// (each container start adds a veth fe80::) reaches no sibling; waking on those
// would restart every offline sibling's backoff and keep it below linkRetryMax.
func addsRoutableAddress(e *ip.EventNetworkAddressChanged) bool {
	return slices.ContainsFunc(e.Added, func(a ip.IP) bool { return a.IsGlobalUnicast() })
}

// signal wakes the loop; a wake already pending absorbs it.
func (a *MaintainLinkTask) signal() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}
