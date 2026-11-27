package user

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeSend allows the user (userOrSelf) to send a message to any
// recipient, and answers no for every other sender.
//
// why here and not in mod/messaging: the rule names the user through
// userOrSelf, which only this module holds.
// note: delivery also needs the recipient's mod.messaging.receive_action.
func (mod *Module) AuthorizeSend(ctx *astral.Context, a *messaging.SendAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}
