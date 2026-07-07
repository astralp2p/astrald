package fs

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/core"
	"github.com/cryptopunkscc/astrald/mod/auth"
	"github.com/cryptopunkscc/astrald/mod/dir"
	"github.com/cryptopunkscc/astrald/mod/objects"
	"github.com/cryptopunkscc/astrald/mod/shell"
)

type Deps struct {
	Auth    auth.Module
	Dir     dir.Module
	Objects objects.Module
	Shell   shell.Module
}

func (mod *Module) LoadDependencies(*astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// add the default repo
	mod.addDefaultRepo()

	// configure repositories from the config file
	for name, cfg := range mod.config.Repos {
		var repo objects.Repository

		if cfg.Writable {
			repo = NewRepository(mod, cfg.Label, cfg.Path)
		} else {
			repo, err = NewWatchRepository(mod, cfg.Path, cfg.Label)
		}
		if err != nil {
			mod.log.Error("error adding repo %v: %v", name, err)
			continue
		}

		mod.Objects.AddRepository(name, repo)
		mod.Objects.AddGroup(objects.RepoLocal, name)

		mod.log.Logv(1, "added repo %v (%v) at %v", name, cfg.Label, cfg.Path)
	}

	return
}
