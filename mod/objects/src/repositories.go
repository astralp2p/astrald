package objects

import (
	"fmt"

	"errors"
	"github.com/astralp2p/astrald/mod/objects"
)

func (mod *Module) AddRepository(name string, repo objects.Repository) error {
	_, ok := mod.repos.Set(name, repo)
	if !ok {
		return fmt.Errorf("repo %s already added", repo.Label())
	}
	return nil
}

func (mod *Module) GetRepository(name string) (repo objects.Repository) {
	repo, _ = mod.repos.Get(name)

	return
}

// RemoveRepository removes the named repository, drops it from every group, and fires its AfterRemoved callback if implemented.
func (mod *Module) RemoveRepository(name string) error {
	if len(name) == 0 {
		return errors.New("name is empty")
	}

	// remove the repo
	removed, ok := mod.repos.Delete(name)
	if !ok {
		return fmt.Errorf("repository %s not found", name)
	}

	// remove the repo from all groups
	mod.groups.Each(func(_ string, group *RepoGroup) error {
		group.Remove(name)
		return nil
	})

	// call the after removed callback
	if c, ok := removed.(objects.AfterRemovedCallback); ok {
		c.AfterRemoved(name)
	}

	return nil
}

func (mod *Module) System() objects.Repository {
	return mod.system
}

// ReadDefault returns the default repository for reading objects
func (mod *Module) ReadDefault() (repo objects.Repository) {
	return mod.GetRepository("main")
}

// WriteDefault returns the default repository for writing objects
func (mod *Module) WriteDefault() (repo objects.Repository) {
	return mod.GetRepository("local")
}

// AddGroup adds a repository to a group
func (mod *Module) AddGroup(groupName string, repoName string) error {
	maybeGroup := mod.GetRepository(groupName)
	if maybeGroup == nil {
		return fmt.Errorf("repo %s not found", groupName)
	}

	group, ok := maybeGroup.(*RepoGroup)
	if !ok {
		return fmt.Errorf("repo %s is not a group", groupName)
	}

	return group.Add(repoName)
}

// RemoveGroup removes a repository from a group
func (mod *Module) RemoveGroup(groupName string, repoName string) error {
	maybeGroup := mod.GetRepository(groupName)
	if maybeGroup == nil {
		return fmt.Errorf("repo %s not found", groupName)
	}

	group, ok := maybeGroup.(*RepoGroup)
	if !ok {
		return fmt.Errorf("repo %s is not a group", groupName)
	}

	return group.Remove(repoName)
}
