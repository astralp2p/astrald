package vfs

import (
	apiauth "github.com/astralp2p/astral-go/api/auth"
	vfsapi "github.com/astralp2p/astral-go/api/vfs"
	"github.com/astralp2p/astral-go/astral"
	authmod "github.com/astralp2p/astrald/mod/auth"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
)

// addAuthorizers registers the module's handlers with the auth module. See VFS 6, section 6.
//
// why: the handlers of the three vfs actions are not NodeLocal, so a contract signed by a VFS identity or a root
// caller is honoured at every level of a chain once the node has indexed it.
//
// why: the see_objects handler is NodeLocal, so a VFS identity cannot hand "read my files by ObjectID" down a
// contract chain (operator decision, 2026-11-12).
func (mod *Module) addAuthorizers() {
	mod.Auth.Add(
		authmod.Func[*vfsapi.ReadAction](mod.AuthorizeReadAction),
		authmod.Func[*vfsapi.WriteAction](mod.AuthorizeWriteAction),
		authmod.Func[*vfsapi.ManageAction](mod.AuthorizeManageAction),
		authmod.NodeLocal(authmod.Func[*apiauth.SeeObjectsAction](mod.AuthorizeSeeObjects)),
	)
}

// AuthorizeReadAction allows the VFS identity to read its own VFS, and a root caller to read any VFS.
func (mod *Module) AuthorizeReadAction(_ *astral.Context, a *vfsapi.ReadAction) bool {
	return mod.isOwnerOrRoot(a.Actor(), a.VFSID)
}

// AuthorizeWriteAction allows the VFS identity to change its own VFS, and a root caller to change any VFS.
func (mod *Module) AuthorizeWriteAction(_ *astral.Context, a *vfsapi.WriteAction) bool {
	return mod.isOwnerOrRoot(a.Actor(), a.VFSID)
}

// AuthorizeManageAction allows a root caller to administer VFSes.
func (mod *Module) AuthorizeManageAction(_ *astral.Context, a *vfsapi.ManageAction) bool {
	return mod.isRootCaller(a.Actor())
}

// isOwnerOrRoot reports whether actor is the non-zero VFS identity or a root caller.
//
// note: the zero identity is never a VFS identity, so a zero VFSID allows no one.
func (mod *Module) isOwnerOrRoot(actor, vfsID *astral.Identity) bool {
	if actor.IsZero() || vfsID.IsZero() {
		return false
	}

	return actor.IsEqual(vfsID) || mod.isRootCaller(actor)
}

// AuthorizeSeeObjects allows reading one object by ObjectID when a file with that ObjectID exists in a VFS the
// actor may read: the actor is that VFS identity, or holds a mod.vfs.read_action permit for it.
//
// The action must name one complete ObjectID. Size 0 is refused, except the ObjectID of empty content.
// The Repo field is ignored. A database error refuses. A refusal is the same for an unknown ObjectID and for an
// ObjectID of another VFS.
//
// note: objects.describe and objects.find ask without an ObjectID (VFS 6, section 6), so this handler refuses them.
func (mod *Module) AuthorizeSeeObjects(ctx *astral.Context, a *apiauth.SeeObjectsAction) bool {
	id := a.ObjectID
	if id == nil || (id.Size == 0 && !objectsmod.IsEmptyObject(id)) {
		return false
	}

	vfsIDs, err := mod.db.findVFSesWithObject(*id)
	if err != nil {
		mod.log.Log("find the VFSes of %v: %v", id, err)
		return false
	}

	for _, vfsID := range vfsIDs {
		if mod.maySeeObjectsIn(ctx, a.Actor(), vfsID) {
			return true
		}
	}

	return false
}

// maySeeObjectsIn reports whether actor is the VFS identity or holds a mod.vfs.read_action permit for it.
//
// note: VFS 6, section 6 "Reading by ObjectID" names no root caller here; the permit is asked of the auth module.
func (mod *Module) maySeeObjectsIn(ctx *astral.Context, actor, vfsID *astral.Identity) bool {
	if actor.IsZero() {
		return false
	}
	if actor.IsEqual(vfsID) {
		return true
	}

	return mod.Auth.Authorize(ctx, &vfsapi.ReadAction{Action: apiauth.NewAction(actor), VFSID: vfsID})
}
