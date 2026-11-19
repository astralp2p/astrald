package nodes

import (
	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeRelayFor grants relaying only when the actor relays for its own identity.
func (mod *Module) AuthorizeRelayFor(ctx *astral.Context, a *nodes.RelayForAction) bool {
	return a.Actor().IsEqual(a.ForID)
}

// admitsRelayer reports whether this node admits relayed queries from relayer
// and takes relayer as a relay candidate (heldRelays). A claimed node admits
// only current members of its local swarm, before any contract is consulted.
// An unclaimed node admits every relayer.
// Spec: .ai/system/topics/app-routing.md, Authorizing the relay.
//
// why: a former member's relay contract stays indexed after it leaves, and the
// contract chain alone would keep admitting it.
//
// why: claimed is read from the user identity, so a swarm lookup fault on a
// claimed node refuses and never reads as an unclaimed node.
func (mod *Module) admitsRelayer(relayer *astral.Identity) bool {
	if mod.User == nil || mod.User.Identity() == nil {
		return true
	}
	return mod.User.IsSwarmMember(relayer)
}
