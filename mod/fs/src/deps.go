package fs

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/shell"
)

type Deps struct {
	Auth    auth.Module
	Dir     dir.Module
	Objects objectsmod.Module
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
		var repo objectsmod.Repository

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
