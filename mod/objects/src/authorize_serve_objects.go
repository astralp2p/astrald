package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// authorizeServeObjects reports whether id may serve objects in role.
//
// The register ops ask this before they add an external provider. Every
// external provider asks it again before it answers a call, and removes itself
// when the answer is no.
//
// why: a registration never outlives the authorization that permitted it.
// Asking on every call removes a provider whose grant was revoked or has
// expired, instead of consulting it until the node restarts.
// note: an external authority that cannot be reached refuses, so it removes a
// permitted provider too. A removed provider is consulted again only after it
// registers again.
func (mod *Module) authorizeServeObjects(ctx *astral.Context, id *astral.Identity, role astral.String8) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(id),
		Role:   role,
	})
}
