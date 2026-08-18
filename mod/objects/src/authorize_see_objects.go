package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeSeeObjects reports whether the query's caller may see objects.
//
// Every read op in this module asks this question and rejects the query when
// the answer is no, before it opens a repository or accepts the connection.
//
// objectID and repo declare the nouns the call touches; either is nil or empty
// for an op that names neither. Nothing evaluates them yet — see
// auth.SeeObjectsAction.
func (mod *Module) authorizeSeeObjects(ctx *astral.Context, q *routing.IncomingQuery, objectID *astral.ObjectID, repo string) bool {
	return mod.Auth.Authorize(ctx, &auth.SeeObjectsAction{
		Action:   auth.NewAction(q.Caller()),
		ObjectID: objectID,
		Repo:     astral.String8(repo),
	})
}
