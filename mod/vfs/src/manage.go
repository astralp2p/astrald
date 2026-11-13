package vfs

import (
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// manageAuthorize checks a query for a manage operation: it must not come from the network, and its caller
// must be a root caller or hold a permit for the manage action. It returns vfsapi.ErrNotPermitted.
//
// why: a manage operation administers every VFS of the node, so a caller off a link has no standing to run it,
// as with the other administration operations of astrald.
func (mod *Module) manageAuthorize(ctx *astral.Context, q *routing.IncomingQuery) error {
	if q.Origin() == astral.OriginNetwork {
		return vfsapi.ErrNotPermitted
	}

	return mod.authorizeManage(ctx, q.Caller())
}
