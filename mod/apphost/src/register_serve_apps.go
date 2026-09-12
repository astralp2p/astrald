package apphost

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
)

// serveAppsGrantRequest returns the grant request of one registration: an
// unconstrained ServeApps permit, followed by the permits the app asked for.
//
// why: a registered app is served from this node, so every registration asks
// for hosting on the grant rail.
// note: the register policy decides whether the grant is written, as it
// decides for every other permit in the request.
func serveAppsGrantRequest(asked string) []*auth.Permit {
	serveApps := &auth.Permit{Action: astral.String8(auth.ServeAppsAction{}.ObjectType())}
	return append([]*auth.Permit{serveApps}, parsePermits(asked)...)
}
