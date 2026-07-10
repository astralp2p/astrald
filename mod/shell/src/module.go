package shell

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/shell"
	"github.com/astralp2p/astrald/resources"
)

var _ shell.Module = &Module{}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	scopes *routing.ScopeRouter
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) String() string {
	return shell.ModuleName
}

func (mod *Module) NewLogAction(message string) shell.LogAction {
	return LogAction{
		mod:     mod,
		message: message,
	}
}
