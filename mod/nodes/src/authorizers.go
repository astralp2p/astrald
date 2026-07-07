package nodes

import (
	"github.com/cryptopunkscc/astral-go/api/nodes"
	"github.com/cryptopunkscc/astral-go/astral"
)

// AuthorizeRelayFor grants relaying only when the actor relays for its own identity.
func (mod *Module) AuthorizeRelayFor(ctx *astral.Context, a *nodes.RelayForAction) bool {
	return a.Actor().IsEqual(a.ForID)
}
