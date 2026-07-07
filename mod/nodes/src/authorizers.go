package nodes

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/nodes"
)

// AuthorizeRelayFor grants relaying only when the actor relays for its own identity.
func (mod *Module) AuthorizeRelayFor(ctx *astral.Context, a *nodes.RelayForAction) bool {
	return a.Actor().IsEqual(a.ForID)
}
