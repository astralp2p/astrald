package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminObjects reports whether the query's caller may destroy what this
// node holds.
//
// Every op in this module that deletes an object, empties a repository, or
// removes one asks this question and rejects the query when the answer is no,
// before it looks the repository up or accepts the connection.
//
// why AdminObjects and not StoreObjects: writing adds and destroying takes away,
// and an issuer granting the first does not thereby mean the second. The two
// handlers differ where it matters — StoreObjects grants the whole local swarm,
// AdminObjects grants the user and this node alone — so a sibling that may push
// an object at this node still may not purge the repository it lands in.
//
// objectID and repo declare the nouns the call touches; objectID is nil for an
// op that names no single object. Nothing evaluates them yet — see
// auth.AdminObjectsAction.
func (mod *Module) authorizeAdminObjects(ctx *astral.Context, q *routing.IncomingQuery, objectID *astral.ObjectID, repo string) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: objectID,
		Repo:     astral.String8(repo),
	})
}
