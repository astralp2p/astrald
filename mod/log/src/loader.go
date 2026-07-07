package log

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/fmt"
	alog "github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/core/assets"
	modlog "github.com/cryptopunkscc/astrald/mod/log"
	"github.com/cryptopunkscc/astrald/mod/log/views"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *alog.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    log,
		assets: assets,
	}

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

	// add a log file to the output list. Failure here (e.g. read-only $HOME on
	// Android) is non-fatal — the module remains usable without on-disk logs.
	if logFile, ferr := CreateLogFile(); ferr != nil {
		log.Error("cannot create log file: %v", ferr)
	} else {
		log.AddLogger(logFile)
	}

	// configure some views
	views.UseQueryView()
	views.UseEntryView()
	views.HideOrigin = node.Identity()

	return mod, err
}

func init() {
	if err := core.RegisterModule(modlog.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
