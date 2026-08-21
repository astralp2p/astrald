package log

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/mod/dir"
	modlog "github.com/astralp2p/astrald/mod/log"
	"github.com/astralp2p/astrald/mod/tree"
	"github.com/astralp2p/astrald/resources"
)

type Deps struct {
	Dir  dir.Module
	Tree tree.Module
}

type Module struct {
	Deps
	config      Config
	node        astral.Node
	log         *log.Logger
	assets      resources.Resources
	logFilePath string
	router      routing.OpRouter
}

func (mod *Module) LogEntryFilter(entry *log.Entry) bool {
	lvl := (*uint8)(mod.config.Level.Get())
	if lvl == nil {
		return entry.Level <= DefaultLogLevel
	}
	return entry.Level <= *lvl
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) String() string {
	return modlog.ModuleName
}
