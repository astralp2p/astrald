package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeSeeSwarm reports whether the query's caller may read the swarm's
// state.
//
// Every read op in this module asks this question and rejects the query with
// code 4 when the answer is no, before it reads the database or accepts the
// connection.
//
// The action declares no nouns: no read op in this module names a single
// subject — each returns the swarm's state whole.
func (mod *Module) authorizeSeeSwarm(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &user.SeeSwarmAction{
		Action: auth.NewAction(q.Caller()),
	})
}
