package user

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/shell"
)

// AuthorizeShell allows the user identity and this node's own identity to open
// an interactive op shell, and nobody else.
//
// why: the rule is authorizeUserOrNode's, shared by the node-wide actions that
// grant no sibling by default. A shell session reaches every module's op
// router, so admission is node-wide administration and a sibling gets it the
// way any other identity does — through a node-local grant or a signed
// contract, never through membership alone.
func (mod *Module) AuthorizeShell(ctx *astral.Context, a *shell.ShellAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
