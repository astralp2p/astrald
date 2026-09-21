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
	mod.addConfigRepos()

	return
}

// addConfigRepos registers every repository named in the config file. A repository that
// cannot be built is logged and skipped; the others are still registered and
// LoadDependencies still succeeds.
//
// why: repoErr is scoped to one iteration. An error scoped to the function survives the
// continue, so a single failed read-only repo skipped every writable repo visited after
// it and returned non-nil, which core/modules.go:120 turns into a panic.
func (mod *Module) addConfigRepos() {
	for name, cfg := range mod.config.Repos {
		var repo objectsmod.Repository
		var repoErr error

		if cfg.Writable {
			repo = NewRepository(mod, cfg.Label, cfg.Path)
		} else {
			repo, repoErr = NewWatchRepository(mod, cfg.Path, cfg.Label)
		}
		if repoErr != nil {
			mod.log.Error("error adding repo %v: %v", name, repoErr)
			continue
		}

		mod.Objects.AddRepository(name, repo)
		mod.Objects.AddGroup(objects.RepoLocal, name)

		mod.log.Logv(1, "added repo %v (%v) at %v", name, cfg.Label, cfg.Path)
	}
}
