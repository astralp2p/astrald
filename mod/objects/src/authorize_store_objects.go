package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeStoreObjects reports whether the query's caller may write objects
// into the node.
//
// Every write op in this module asks this question and rejects the query when
// the answer is no, before it opens a repository or accepts the connection.
//
// repo and objectType declare the nouns the call touches; both are empty for an
// op that names neither, and for an op whose subject arrives on the channel
// after the decision is made. Nothing evaluates them yet — see
// auth.StoreObjectsAction.
func (mod *Module) authorizeStoreObjects(ctx *astral.Context, q *routing.IncomingQuery, repo string, objectType string) bool {
	return mod.Auth.Authorize(ctx, &auth.StoreObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(repo),
		Type:   astral.String8(objectType),
	})
}
