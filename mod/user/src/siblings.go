package user

import (
	"context"

	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Sibling is a link-maintenance entry for one swarm peer: the tracked node plus
// the Cancel that stops its background link-maintenance goroutine.
type Sibling struct {
	ID     *astral.Identity
	Cancel context.CancelFunc
}

func (mod *Module) notifySiblings(event string) {
	ac := mod.ActiveContract()
	if ac == nil {
		return
	}

	for _, sib := range mod.getSiblings() {
		sib := sib
		go mod.Objects.Push(mod.ctx, sib, &user.Notification{Event: astral.String8(event)})
	}
}

// getSiblings returns the swarm members this node currently holds a live link
// with, excluding the local node. Membership comes from LocalSwarm (contract-
// derived, expel-aware); liveness from Nodes.IsLinked. It is independent of the
// sibs maintenance registry, so a stale or not-yet-linked entry never leaks through.
func (mod *Module) getSiblings() (list []*astral.Identity) {
	self := mod.node.Identity()

	for _, node := range mod.LocalSwarm() {
		if node.IsEqual(self) || !mod.Nodes.IsLinked(node) {
			continue
		}
		list = append(list, node)
	}

	return
}

func (mod *Module) addSibling(id *astral.Identity, cancel context.CancelFunc) {
	mod.log.Log("adding sibling %v", id)
	mod.sibs.Set(id.String(), Sibling{
		ID:     id,
		Cancel: cancel,
	})
}

func (mod *Module) removeSibling(id *astral.Identity) {
	sib, ok := mod.sibs.Delete(id.String())
	if !ok {
		return
	}

	mod.log.Log("removing sibling %v", id)
	sib.Cancel()
}
