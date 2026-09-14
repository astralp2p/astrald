package objects

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// authorizeServeObjects reports whether id may serve objects in role.
//
// The register ops ask this before they add an external provider. Every
// external provider asks it again before it answers a call, and sits the call
// out when the answer is no.
//
// why: a registration outlives the grant that permitted it. Asking on every
// call takes a provider whose grant was revoked or has expired out of the
// answer path, instead of consulting it until the node restarts.
// why: a refused provider stays registered. A refusal can be transient — an
// external authority that cannot be reached refuses — and removing on it would
// drop a permitted provider until it registers again.
func (mod *Module) authorizeServeObjects(ctx *astral.Context, id *astral.Identity, role astral.String8) bool {
	return mod.Auth.Authorize(ctx, &auth.ServeObjectsAction{
		Action: auth.NewAction(id),
		Role:   role,
	})
}
