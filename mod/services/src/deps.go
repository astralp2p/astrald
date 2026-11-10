package services

import (
	"github.com/astralp2p/astral-go/api/services"
	treeapi "github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	apphostmod "github.com/astralp2p/astrald/mod/apphost"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/mod/user"
)

type Deps struct {
	Apphost apphostmod.Module
	Auth    authmod.Module
	Objects objects.Module
	Tree    tree.Module
	User    user.Module
}

// visibilityPath is the tree node holding the services settings.
const visibilityPath = "/mod/services"

// visibilityConfig is the user's visibility setting in the node's tree.
//
// why apart from Module: a tree.Value carries a mutex and binds by reflection
// over its own struct.
type visibilityConfig struct {
	Private treeapi.Value[*services.PrivateList] // at /mod/services/private
}

func (mod *Module) LoadDependencies(ctx *astral.Context) error {
	if err := core.Inject(mod.node, &mod.Deps); err != nil {
		return err
	}
	return mod.bindVisibility(ctx)
}

// bindVisibility binds the private list in the tree and applies it.
func (mod *Module) bindVisibility(ctx *astral.Context) error {
	if err := tree.BindPath(ctx, &mod.visibility, mod.Tree.Root(), visibilityPath, true); err != nil {
		return err
	}
	// why: the list applies before Run, so no discovery sees a hidden provider
	// in between.
	mod.coord.SetPrivate(mod.visibility.Private.Get())
	return nil
}
