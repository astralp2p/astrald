package messaging

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/apphost"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/dir"
	"github.com/astralp2p/astrald/mod/objects"
)

type Deps struct {
	Apphost apphost.Module
	Auth    auth.Module
	Crypto  crypto.Module
	Dir     dir.Module
	Objects objects.Module
}

// OptionalDeps are the modules messaging runs without.
type OptionalDeps struct {
	User siblingPusher
}

// siblingPusher is the part of the user module messaging uses.
type siblingPusher interface {
	PushToSiblings(ctx *astral.Context, obj astral.Object)
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	// why the context is taken here and not in Run: mcp calls into this module
	// from its own Run, and the Run stages start with no order between modules.
	// Deliveries may reach remote participants, so the context carries the
	// network zone like apphost's does.
	mod.ctx = ctx.IncludeZone(astral.ZoneNetwork)

	if err = core.Inject(mod.node, &mod.Deps); err != nil {
		return err
	}

	// optional: without the user module a new mailbox's relay contract reaches
	// a sibling alone at its next first link or once a new link to it replaced a dead one.
	core.Inject(mod.node, &mod.OptionalDeps)

	mod.addAuthorizers()

	return nil
}
