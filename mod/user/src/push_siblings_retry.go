package user

import (
	"errors"
	"slices"
	"time"

	objectsapi "github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/objects"
)

// siblingPushBackoff lists the waits before the retries of a push that failed
// to reach a sibling: one wait per retry, so a push has 1 + len pushes at most.
// why bounded: the sibling gets the contract again from every later sync of it
// (SyncNodesTask), so a long retry would repeat what those do.
var siblingPushBackoff = []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute}

// PushToSiblingsRetry sends obj to every linked sibling like PushToSiblings,
// and sends it again, up to len(siblingPushBackoff) times on the backoff, to a
// sibling the push did not reach.
//
// A sibling's retries stop at its first answer (accepted or refused), when it
// leaves getSiblings, or when ctx ends.
// why a refusal stops it: the sibling received obj and refused it; the same
// object is refused again. A later sync sends it once the sibling's state
// changed.
func (mod *Module) PushToSiblingsRetry(ctx *astral.Context, obj astral.Object) {
	for _, sib := range mod.getSiblings() {
		go mod.pushToSiblingRetry(ctx, sib, obj)
	}
}

// pushToSiblingRetry pushes obj to sib, retrying on siblingPushBackoff.
func (mod *Module) pushToSiblingRetry(ctx *astral.Context, sib *astral.Identity, obj astral.Object) {
	for retry := 0; ; retry++ {
		err := mod.pushOnce(ctx, sib, obj)
		switch {
		case err == nil, ctx.Err() != nil:
			return
		case isPushRejected(err):
			mod.log.Logv(1, "sibling %v rejected the pushed %v", sib, obj.ObjectType())
			return
		case retry >= len(siblingPushBackoff):
			mod.log.Logv(1, "gave up pushing %v to sibling %v after %v pushes: %v", obj.ObjectType(), sib, retry+1, err)
			return
		}

		wait := siblingPushBackoff[retry]
		mod.log.Logv(1, "push %v to sibling %v failed, retrying in %v: %v", obj.ObjectType(), sib, wait, err)
		select {
		case <-ctx.Done():
			return
		case <-mod.after(wait):
		}

		// why after the wait: the sibling can leave the swarm or lose its link
		// while the retry waits.
		if !slices.ContainsFunc(mod.getSiblings(), sib.IsEqual) {
			return
		}
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
