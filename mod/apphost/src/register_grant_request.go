package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// registerGrantRequest returns the grant request of one registration:
// unconstrained ServeApps and UseBlueprints permits, followed by the permits the
// app asked for.
//
// why: a registered app is served from this node and syncs the blueprints of
// its types (apps.WithBlueprintSync), so every registration asks for hosting
// and for blueprints on the grant rail.
// note: the register policy decides whether the grant is written, as it
// decides for every other permit in the request.
func registerGrantRequest(asked string) []*auth.Permit {
	serveApps := &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}
	useBlueprints := &auth.Permit{Action: astral.String8(auth.UseBlueprintsAction{}.ObjectType())}
	return append([]*auth.Permit{serveApps, useBlueprints}, parsePermits(asked)...)
}
