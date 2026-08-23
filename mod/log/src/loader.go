package log

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/fmt"
	alog "github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	modlog "github.com/astralp2p/astrald/mod/log"
	"github.com/astralp2p/astrald/mod/log/views"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *alog.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    log,
		assets: assets,
	}

	mod.config.setDefaults()
	_ = assets.LoadYAML(modlog.ModuleName, &mod.config)

	err = mod.router.AddStructPrefix(mod, "Op")
	if err != nil {
		return nil, err
	}

	fmt.SetView(func(identity *astral.Identity) fmt.View {
		return views.IdentityView{
			Identity:  identity,
			Highlight: identity.IsEqual(node.Identity()),
		}
	})

	// set the log filter
	log.SetFilter(mod.LogEntryFilter)

	addLogFile(log, node.Identity(), assets.Res(), &mod.config)

	// configure some views
	views.UseQueryView()
	views.UseEntryView()
	views.HideOrigin = node.Identity()

	return mod, err
}

// addLogFile adds a log file to the output list. Failure here (e.g. a
// read-only root) is non-fatal — the module remains usable without on-disk
// logs.
// why: the logs directory is created only when the file is wanted, so a node
// whose output is already collected leaves nothing behind.
func addLogFile(log *alog.Logger, origin *astral.Identity, res resources.Resources, config *Config) {
	if !config.File {
		return
	}

	// note: FileResources is the only backend with a filesystem behind it; a
	// memory-backed node (ghost mode) has no root to write to.
	files, ok := res.(*resources.FileResources)
	if !ok {
		return
	}

	logFile, err := CreateLogFile(origin, files.DataRoot(), config.FileMaxSize, config.FileMaxFiles)
	if err != nil {
		log.Error("cannot create log file: %v", err)
		return
	}

	log.AddLogger(logFile)
}

func init() {
	if err := core.RegisterModule(modlog.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
