package user

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/user"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminSwarm reports whether the query's caller may change what the
// swarm holds.
//
// Every mutating op in this module asks this question and rejects the query
// with code 4 when the answer is no, before it changes any state.
//
// subject and objectID declare the nouns the call touches — subject the node a
// call adopts or expels, objectID the asset it adds or removes. Each is nil for
// an op that names the other. Nothing evaluates them yet — see
// user.AdminSwarmAction.
func (mod *Module) authorizeAdminSwarm(
	ctx *astral.Context,
	q *routing.IncomingQuery,
	subject *astral.Identity,
	objectID *astral.ObjectID,
) bool {
	return mod.Auth.Authorize(ctx, &user.AdminSwarmAction{
		Action:   auth.NewAction(q.Caller()),
		Subject:  subject,
		ObjectID: objectID,
	})
}
