package indexing

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeServeIndexer reports whether the query's caller may hold an indexer
// registration: take a name, and consume the change stream it names.
func (mod *Module) authorizeServeIndexer(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(q.Caller()),
		Role:   auth.RoleIndexer,
	})
}

// authorizeAdminObjects reports whether the query's caller may delete a
// registration another identity owns.
//
// why: deleting a registration destroys its cursors, and an op that destroys
// object-domain state answers to AdminObjects. The call names no object,
// repository or path.
func (mod *Module) authorizeAdminObjects(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.AdminObjectsAction{
		Action: auth.NewAction(q.Caller()),
	})
}
