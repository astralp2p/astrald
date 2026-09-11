package coldcard

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/coldcard"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeScan reports whether the query's caller may scan the attached
// Coldcard devices.
//
// note: coldcard.scan asks before it accepts the query, so a refused caller
// receives no bytes and no device tool runs.
// note: the action grants scanning alone; no signing follows from it.
func (mod *Module) authorizeScan(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &coldcard.ScanAction{
		Action: auth.NewAction(q.Caller()),
	})
}
