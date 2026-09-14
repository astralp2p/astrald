package indexing

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeStoreObjects reports whether the query's caller may change what this
// node indexes.
//
// Indexing state is node-wide: enabling a repository indexes every object in it
// from then on. That is a write to node state, so it answers to StoreObjects —
// the same action that guards writes in mod/objects.
//
// repo declares the noun the call touches. Nothing evaluates it yet — see
// auth.StoreObjectsAction.
func (mod *Module) authorizeStoreObjects(ctx *astral.Context, q *routing.IncomingQuery, repo string) bool {
	return mod.Auth.Authorize(ctx, &auth.StoreObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Repo:   astral.String8(repo),
	})
}
