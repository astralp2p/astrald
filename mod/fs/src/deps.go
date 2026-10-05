package fs

import (
	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/dir"
	objectsmod "github.com/astralp2p/astrald/mod/objects"
	"github.com/astralp2p/astrald/mod/shell"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

type Deps struct {
	Auth    auth.Module
	Dir     dir.Module
	Objects objectsmod.Module
	Shell   shell.Module
	Tree    treemod.Module
}

func (mod *Module) LoadDependencies(ctx *astral.Context) (err error) {
	err = core.Inject(mod.node, &mod.Deps)
	if err != nil {
		return
	}

	// add the default repo
	mod.addDefaultRepo()

	// configure repositories from the config file
	mod.addConfigRepos()

	// restore the repositories persisted in the tree
	mod.addTreeRepos(ctx)

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
		repo, repoErr := mod.newRepo(cfg, false)
		if repoErr != nil {
			mod.log.Error("error adding repo %v: %v", name, repoErr)
			continue
		}

		mod.Objects.AddRepository(name, repo)
		mod.Objects.AddGroup(objects.RepoLocal, name)

		mod.log.Logv(1, "added repo %v (%v) at %v", name, cfg.Label, cfg.Path)
	}
}

// addTreeRepos registers every repository persisted in the tree. An entry whose name is already
// registered, by the config file or the default repo, is skipped. An entry that cannot be built,
// including one whose path is gone, is logged and skipped, and its tree entry stays.
//
// why: validPath runs on every entry because tree.set needs a weaker grant than the ops that
// attach a directory, so a tree entry is not trusted to name a usable directory.
func (mod *Module) addTreeRepos(ctx *astral.Context) {
	repos, err := mod.loadRepos(ctx)
	if err != nil {
		mod.log.Error("error loading persisted repos: %v", err)
		return
	}

	for name, cfg := range repos {
		if mod.Objects.GetRepository(name) != nil {
			mod.log.Error("persisted repo %v skipped: name already registered", name)
			continue
		}

		path, err := validPath(cfg.Path)
		if err != nil {
			mod.log.Error("persisted repo %v skipped: %v", name, err)
			continue
		}
		cfg.Path = path

		repo, err := mod.newRepo(cfg, true)
		if err != nil {
			mod.log.Error("error adding persisted repo %v: %v", name, err)
			continue
		}

		mod.Objects.AddRepository(name, repo)
		mod.Objects.AddGroup(objects.RepoLocal, name)

		mod.log.Logv(1, "added persisted repo %v (%v) at %v", name, cfg.Label, cfg.Path)
	}
}

// newRepo builds the repository cfg describes. persisted marks a repository whose tree entry
// is deleted when the repository is removed.
func (mod *Module) newRepo(cfg RepoConfig, persisted bool) (objectsmod.Repository, error) {
	if cfg.Writable {
		repo := NewRepository(mod, cfg.Label, cfg.Path)
		repo.persisted = persisted
		return repo, nil
	}

	repo, err := NewWatchRepository(mod, cfg.Path, cfg.Label)
	if err != nil {
		return nil, err
	}
	repo.persisted = persisted

	return repo, nil
}
