package services

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminNetwork reports whether the query's caller may administer this
// node's network.
//
// Every guarded op in this module asks before it reads network state, schedules
// work, opens a socket, consumes a hole, or changes state, and rejects the query
// when the answer is no.
//
// note: an op's peer, session and hole checks stay in force after this check.
// note: this check answers permission, and those checks answer protocol validity.
func (mod *Module) authorizeAdminNetwork(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminNetworkAction{Action: auth.NewAction(q.Caller())})
}
