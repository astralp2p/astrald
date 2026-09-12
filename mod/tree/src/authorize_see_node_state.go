package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeSeeNodeState reports whether the query's caller may read the node's
// tree.
//
// note: tree.get and tree.list ask before they accept the query, walk a path, or
// follow a value, and reject the query when the answer is no.
// note: a path under a remote mount is walked on the remote node, so the guard
// also keeps a refused caller from making this node query another one.
func (mod *Module) authorizeSeeNodeState(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.SeeNodeStateAction{
		Action: auth.NewAction(q.Caller()),
	})
}
