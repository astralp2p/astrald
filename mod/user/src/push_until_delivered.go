package user

import (
	"errors"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// The backoff between failed pushes in pushUntilDelivered: it starts at
// pushRetryMin and doubles after every failure up to pushRetryMax, the link
// backoff's bounds (maintain_link_task.go).
const (
	pushRetryMin = time.Second
	pushRetryMax = 15 * time.Minute
)

// pushAttempts bounds the pushes to a target that never answers: about 22 hours
// of retries at the backoff above.
// why a count: a ban never expires, so its lifetime bounds nothing.
const pushAttempts = 100

// pushUntilDelivered pushes obj to target until target answers, waiting on the
// backoff between pushes that fail to reach it. It stops at the first answer,
// after pushAttempts pushes, or when ctx ends, and reports whether target
// answered: accepted obj or refused it.
// why a rejection stops it: target received obj and refused it; the same object
// is refused again.
func (mod *Module) pushUntilDelivered(ctx *astral.Context, target *astral.Identity, obj astral.Object) (answered bool) {
	delay := pushRetryMin
	for attempt := 1; ; attempt++ {
		err := mod.pushOnce(ctx, target, obj)
		switch {
		case err == nil:
			return true
		case ctx.Err() != nil:
			return false
		case isPushRejected(err):
			mod.log.Logv(1, "%v rejected the pushed %v", target, obj.ObjectType())
			return true
		case attempt >= pushAttempts:
			mod.log.Logv(1, "gave up pushing %v to %v after %v attempts: %v", obj.ObjectType(), target, attempt, err)
			return false
		}

		mod.log.Logv(1, "push %v to %v failed, retrying in %v: %v", obj.ObjectType(), target, delay, err)
		select {
		case <-ctx.Done():
			return false
		case <-mod.after(delay):
		}
		delay = min(delay*2, pushRetryMax)
	}
}

// pushOnce pushes obj to target, bounded by siblingPushTimeout.
func (mod *Module) pushOnce(ctx *astral.Context, target *astral.Identity, obj astral.Object) error {
	pctx, cancel := ctx.WithTimeout(siblingPushTimeout)
	defer cancel()

	return mod.Objects.Push(pctx, target, obj)
}

// isPushRejected reports whether err is a receiver's refusal of a pushed object.
// why both: the objects client reports a remote refusal with the wire error, and
// a push to this node itself returns the local receiver's error.
func isPushRejected(err error) bool {
	return errors.Is(err, objectsapi.ErrPushRejected) || errors.Is(err, objects.ErrPushRejected)
}
