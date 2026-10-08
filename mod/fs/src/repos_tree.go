package fs

import (
	"errors"
	"strings"

	"github.com/astralp2p/astral-go/api/tree"
	"github.com/astralp2p/astral-go/astral"
	treemod "github.com/astralp2p/astrald/mod/tree"
)

// reposTreePath holds one subnode per persisted repository. A subnode holds the
// RepoConfig fields as the child nodes `path`, `label` and `writable`.
const reposTreePath = "/mod/fs/repos"

const (
	pathKey     = "path"
	labelKey    = "label"
	writableKey = "writable"
)

var errInvalidRepoName = errors.New("repository name must not be empty or contain '/'")

// saveRepo writes cfg to the tree under name, replacing any previous entry.
func (mod *Module) saveRepo(ctx *astral.Context, name string, cfg RepoConfig) error {
	// why: a '/' in the name would split the entry into nested nodes.
	if name == "" || strings.Contains(name, "/") {
		return errInvalidRepoName
	}

	node, err := treemod.Query(ctx, mod.Tree.Root(), reposTreePath+"/"+name, true)
	if err != nil {
		return err
	}

	for key, value := range map[string]astral.Object{
		pathKey:     astral.NewString8(cfg.Path),
		labelKey:    astral.NewString8(cfg.Label),
		writableKey: (*astral.Bool)(&cfg.Writable),
	} {
		field, err := treemod.Query(ctx, node, key, true)
		if err != nil {
			return err
		}

		if err = field.Set(ctx, value); err != nil {
			return err
		}
	}

	return nil
}

// deleteRepo removes the tree entry of name. A missing entry is not an error.
func (mod *Module) deleteRepo(ctx *astral.Context, name string) error {
	entry, err := tree.Query(ctx, mod.Tree.Root(), reposTreePath+"/"+name, false)
	if err != nil {
		return nil
	}

	fields, err := entry.Sub(ctx)
	if err != nil {
		return err
	}

	for _, field := range fields {
		if err = field.Delete(ctx); err != nil {
			return err
		}
	}

	return entry.Delete(ctx)
}

// loadRepos reads every persisted entry. An entry that cannot be read is logged and skipped.
func (mod *Module) loadRepos(ctx *astral.Context) (map[string]RepoConfig, error) {
	root, err := treemod.Query(ctx, mod.Tree.Root(), reposTreePath, true)
	if err != nil {
		return nil, err
	}

	entries, err := root.Sub(ctx)
	if err != nil {
		return nil, err
	}

	repos := map[string]RepoConfig{}
	for name, entry := range entries {
		cfg, err := readRepo(ctx, entry)
		if err != nil {
			mod.log.Error("error reading persisted repo %v: %v", name, err)
			continue
		}

		repos[name] = cfg
	}

	return repos, nil
}

func readRepo(ctx *astral.Context, entry tree.Node) (cfg RepoConfig, err error) {
	fields, err := entry.Sub(ctx)
	if err != nil {
		return cfg, err
	}

	path, err := readField[*astral.String8](ctx, fields, pathKey)
	if err != nil {
		return cfg, err
	}

	label, err := readField[*astral.String8](ctx, fields, labelKey)
	if err != nil {
		return cfg, err
	}

	writable, err := readField[*astral.Bool](ctx, fields, writableKey)
	if err != nil {
		return cfg, err
	}

	return RepoConfig{Label: string(*label), Path: string(*path), Writable: bool(*writable)}, nil
}

func readField[T astral.Object](ctx *astral.Context, fields map[string]tree.Node, key string) (v T, err error) {
	node, ok := fields[key]
	if !ok {
		return v, errors.New("missing " + key)
	}

	return tree.Get[T](ctx, node)
}

// deletePersisted deletes the tree entry of a removed repository when persisted is true.
//
// why: AfterRemoved cannot return an error, so a failed delete is logged. The entry then
// returns the repository on the next start.
func (mod *Module) deletePersisted(name string, persisted bool) {
	if !persisted {
		return
	}

	if err := mod.deleteRepo(mod.ctx, name); err != nil {
		mod.log.Error("error deleting persisted repo %v: %v", name, err)
	}
}

// persistAdded saves a repository just added under name. A failed save removes the repository again.
//
// why: a repository that is live but not saved would vanish at the next restart with no trace
// in the reply.
func (mod *Module) persistAdded(ctx *astral.Context, name string, cfg RepoConfig) error {
	err := mod.saveRepo(ctx, name, cfg)
	if err == nil {
		return nil
	}

	if rmErr := mod.Objects.RemoveRepository(name); rmErr != nil {
		mod.log.Error("error rolling back repo %v: %v", name, rmErr)
	}

	return err
}
