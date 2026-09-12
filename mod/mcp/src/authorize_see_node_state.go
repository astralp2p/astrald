package mcp

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeSeeNodeState reports whether the query's caller may read agent
// metadata.
//
// note: mcp.agent asks after its origin refusal and before it accepts the query
// or reads an agent's record, and rejects the query when the answer is no.
func (mod *Module) authorizeSeeNodeState(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.SeeNodeStateAction{
		Action: auth.NewAction(q.Caller()),
	})
}
