package sys

import (
	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/tree"
)

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return
	}
	mod.apps = mod.Apphost
	mod.apphostPolicies = mod.Apphost

	// why NoLocal: an appointment that is not saved must fail, not survive in
	// memory until a restart reopens fresh.
	mod.appointment.Manager.NoLocal = true
	if err = tree.BindPath(ctx, &mod.appointment, mod.Tree.Root(), appointmentPath, true); err != nil {
		return
	}
	if err = tree.BindPath(ctx, &mod.legacy, mod.Tree.Root(), legacyAppointmentPath, true); err != nil {
		return
	}

	// optional — sys runs without the user module
	core.Inject(mod.node, &mod.OptionalDeps)

	// why NodeLocal: the manager is a record of this node, and a contract chain
	// must not extend it a hop.
	mod.Auth.Add(authmod.NodeLocal(authmod.Func[*auth.ConfigureNodeStateAction](mod.AuthorizeConfigureNodeState)))

	return
}
