package user

import (
	"github.com/astralp2p/astral-go/astral"
)

// getSiblings returns the swarm members this node currently holds a live link
// with, excluding the local node. Membership comes from LocalSwarm (contract-
// derived, expel-aware); liveness from Nodes.IsLinked.
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
