package user

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeReceive allows the user (userOrSelf), as a recipient, to take a
// message from any sender, and answers no for every other recipient.
//
// why here and not in mod/messaging: the rule names the user through
// userOrSelf, which only this module holds.
func (mod *Module) AuthorizeReceive(ctx *astral.Context, a *messaging.ReceiveAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}
