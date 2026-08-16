package fs

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeAdminObjects reports whether the query's caller may attach a
// repository to this node.
//
// Both ops in this module ask this question and reject the query when the answer
// is no, before they validate the path or accept the connection.
//
// repo and path declare the nouns the call touches. Nothing evaluates them yet —
// see auth.AdminObjectsAction.
func (mod *Module) authorizeAdminObjects(ctx *astral.Context, q *routing.IncomingQuery, repo string, path string) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(repo),
		Path:   astral.String8(path),
	})
}
