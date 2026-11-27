package user

import (
	"time"

	"github.com/astralp2p/astral-go/astral"
)

// note: this sweep, the expulsion sync with a banned subject and the sibling sync
// on every link (object_receiver.go) are an interim repair of missed membership
// pushes; Universal Object Delivery supersedes them
// (https://wiki.satforge.dev/doc/universal-object-delivery-Qo2EsUffMl).

// defaultReconcileInterval is the sweep period when user.yaml sets none.
const defaultReconcileInterval = time.Hour

// reconcileTimeout bounds one expulsion sync with one peer.
const reconcileTimeout = 30 * time.Second

// runReconcile runs the expulsion sweep every reconcile interval until ctx ends.
func (mod *Module) runReconcile(ctx *astral.Context) {
	ticker := time.NewTicker(mod.reconcileInterval())
	defer ticker.Stop()

	mod.reconcileOnTick(ctx, ticker.C)
}

// reconcileInterval returns the configured sweep period, or the default when
// user.yaml sets a zero or negative one.
func (mod *Module) reconcileInterval() time.Duration {
	if mod.file.ReconcileInterval <= 0 {
		return defaultReconcileInterval
	}
	return mod.file.ReconcileInterval
}

// reconcileOnTick runs one sweep per tick until ctx ends.
func (mod *Module) reconcileOnTick(ctx *astral.Context, tick <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			for _, peer := range mod.reconcileTargets() {
				go mod.reconcileWith(ctx, peer)
			}
		}
	}
}

// reconcileTargets returns the linked siblings and the linked subjects of the
// active user's bans.
// why: a member whose link never drops raises no new LinkCreatedEvent, nor does
// a subject whose link predates its ban.
func (mod *Module) reconcileTargets() []*astral.Identity {
	targets := mod.getSiblings()

	ac := mod.ActiveContract()
	if ac == nil {
		return targets
	}

	for _, signed := range mod.snapshot().Expulsions() {
		subject := signed.Subject
		if subject.IsEqual(mod.node.Identity()) || !mod.Nodes.IsLinked(subject) {
			continue
		}
		targets = append(targets, subject)
	}
	return targets
}

// reconcileWith pushes the active user's bans to peer, bounded by reconcileTimeout.
func (mod *Module) reconcileWith(ctx *astral.Context, peer *astral.Identity) {
	sctx, cancel := ctx.WithTimeout(reconcileTimeout)
	defer cancel()

	mod.syncExpulsions(sctx, peer)
}
