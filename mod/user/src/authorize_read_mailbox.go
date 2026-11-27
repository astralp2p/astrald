package user

import (
	"github.com/astralp2p/astral-go/api/messaging"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeReadMailbox allows the user (userOrSelf) to read any mailbox this
// node hosts, and answers no for every other reader.
//
// why here and not in mod/messaging: the rule names the user through
// userOrSelf, which only this module holds.
// note: no contract carries the action (messaging.ReadMailboxAction), so the
// rule answers for the user alone and delegates nothing.
// note: mod/messaging refuses this node's own identity as a reader before it
// asks (admitsReader), so an unclaimed node reads no other mailbox.
func (mod *Module) AuthorizeReadMailbox(ctx *astral.Context, a *messaging.ReadMailboxAction) bool {
	return !a.Actor().IsZero() && a.Actor().IsEqual(mod.userOrSelf())
}
