package tree

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// authorizeConfigureNodeState reports whether the query's caller may change this
// node's tree.
//
// Every op in this module that writes a value, deletes a node, mounts a remote
// tree, or removes a mount asks this question and rejects the query when the
// answer is no, before it accepts the connection or touches a node.
//
// why: the check sits at the op and not in Module.Set, Delete, Mount or Unmount,
// because those helpers also serve in-process callers, which carry no query
// caller to authorize.
func (mod *Module) authorizeConfigureNodeState(ctx *astral.Context, q *routing.IncomingQuery) bool {
	return mod.Auth.Authorize(ctx, &auth.ConfigureNodeStateAction{
		Action: auth.NewAction(q.Caller()),
	})
}
