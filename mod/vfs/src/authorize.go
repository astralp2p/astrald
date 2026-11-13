package vfs

import (
	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/routing"
)

// access is what an operation asks of a VFS.
type access int

const (
	accessRead access = iota
	accessWrite
)

// isRootCaller reports whether a caller needs no permit: the node's user, and the node. On a node without a user,
// or whose user identity is zero, the node is the only root caller.
func (mod *Module) isRootCaller(caller *astral.Identity) bool {
	if caller.IsZero() {
		return false
	}

	if caller.IsEqual(mod.identity) {
		return true
	}

	return mod.User != nil && caller.IsEqual(mod.User.Identity())
}

// authorizeVFS checks the caller in the order of VFS 6, section 6: authorization first, then whether the VFS
// exists. The caller of this function checks the path after it.
//
// A read or a write is allowed when the caller is the VFS identity, or a root caller, or holds a permit for the
// action with the VFS in its constraints. A write does not imply a read. The zero identity is never allowed.
// The errors are vfsapi.ErrNotPermitted and vfsapi.ErrNoSuchVFS.
func (mod *Module) authorizeVFS(ctx *astral.Context, caller, vfsID *astral.Identity, want access) (*dbVolume, error) {
	if !mod.mayAccess(ctx, caller, vfsID, want) {
		return nil, vfsapi.ErrNotPermitted
	}

	return mod.db.findVolume(vfsID)
}

// mayAccess reports whether a caller may read or write a VFS: the caller is the VFS identity, or a root caller, or
// holds a permit for the action with the VFS in its constraints. The zero identity may do nothing.
func (mod *Module) mayAccess(ctx *astral.Context, caller, vfsID *astral.Identity, want access) bool {
	switch {
	case caller.IsZero():
		return false
	case caller.IsEqual(vfsID), mod.isRootCaller(caller):
		return true
	}

	action := apiauth.NewAction(caller)

	switch want {
	case accessRead:
		return mod.Auth.Authorize(ctx, &vfsapi.ReadAction{Action: action, VFSID: vfsID})
	case accessWrite:
		return mod.Auth.Authorize(ctx, &vfsapi.WriteAction{Action: action, VFSID: vfsID})
	}

	return false
}

// authorizeManage checks the caller for an administration operation: a root caller, or a holder of a permit for
// the manage action. It returns vfsapi.ErrNotPermitted.
//
// note: a manage operation also refuses a query from the network; the operation checks the origin.
func (mod *Module) authorizeManage(ctx *astral.Context, caller *astral.Identity) error {
	switch {
	case caller.IsZero():
	case mod.isRootCaller(caller):
		return nil
	case mod.Auth.Authorize(ctx, &vfsapi.ManageAction{Action: apiauth.NewAction(caller)}):
		return nil
	}

	return vfsapi.ErrNotPermitted
}

// authorizeVFSQuery authorizes the caller of a query on the VFS the query names and returns it. A nil vfsArg names
// the caller.
//
// The caller checks the path after this call, as VFS 6, section 6 orders it.
func (mod *Module) authorizeVFSQuery(ctx *astral.Context, q *routing.IncomingQuery, vfsArg *astral.Identity, want access) (*dbVolume, error) {
	if vfsArg == nil {
		vfsArg = q.Caller()
	}

	return mod.authorizeVFS(ctx, q.Caller(), vfsArg, want)
}

// authorizeManageQuery checks a query for a manage operation: it must not come from the network, and its caller
// must be a root caller or hold a permit for the manage action. It returns vfsapi.ErrNotPermitted.
//
// why: a manage operation administers every VFS of the node, so a caller off a link has no standing to run it,
// as with the other administration operations of astrald.
func (mod *Module) authorizeManageQuery(ctx *astral.Context, q *routing.IncomingQuery) error {
	if q.Origin() == astral.OriginNetwork {
		return vfsapi.ErrNotPermitted
	}

	return mod.authorizeManage(ctx, q.Caller())
}
