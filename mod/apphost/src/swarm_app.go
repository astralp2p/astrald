package apphost

import (
	"slices"

	"github.com/astralp2p/astral-go/api/nodes"
	"github.com/astralp2p/astral-go/astral"
)

// IsSwarmApp reports whether app is hosted on a node of the local swarm. A
// host is the subject of an active relay-for contract the app issued, the same
// lookup OpHosts answers.
//
// why: a contract is indexed only when issuer and subject both signed it, so
// only a swarm node can be the subject of an app's contract naming it.
func (mod *Module) IsSwarmApp(ctx *astral.Context, app *astral.Identity) bool {
	if app == nil || app.IsZero() || mod.User == nil {
		return false
	}
	swarm := mod.User.LocalSwarm()
	if len(swarm) == 0 {
		return false
	}
	contracts, err := mod.Auth.SignedContracts().
		WithIssuer(app).
		WithAction(&nodes.RelayForAction{}).
		Find(ctx.ExcludeZone(astral.ZoneNetwork))
	if err != nil {
		// why: a lookup fault must not read as membership.
		mod.log.Errorv(1, "error finding hosts of %v: %v", app, err)
		return false
	}
	for _, c := range contracts {
		if c.Subject != nil && slices.ContainsFunc(swarm, c.Subject.IsEqual) {
			return true
		}
	}
	return false
}
