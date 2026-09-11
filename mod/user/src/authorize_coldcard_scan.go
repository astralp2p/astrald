package user

import (
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
)

// AuthorizeColdcardScan allows the user identity and this node's own identity to
// scan the node's attached Coldcard devices, and nobody else.
//
// why: a scan runs the device tool against every attached Coldcard and refreshes
// the device map the coldcard crypto engine signs from, so no sibling or app
// holds it by default.
// why: the setup ceremony scans on an unclaimed node as an anonymous caller, and
// the router promotes that caller to this node's identity (core/router.go).
func (mod *Module) AuthorizeColdcardScan(ctx *astral.Context, a *coldcard.ScanAction) bool {
	return mod.authorizeUserOrNode(a.Actor())
}
