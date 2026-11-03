package objects

import (
	"errors"

	"github.com/astralp2p/astral-go/api/objects"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/objects/mem"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var mod = &Module{
		node:   node,
		config: defaultConfig,
		log:    log,
	}

	if err := assets.LoadYAML(objects.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	// why: the deps scan skips this module (deps.go), so its own finder is added here.
	mod.AddFinder(mod)

	mod.db = &DB{DB: assets.Database()}
	mod.objectsReadsJournal = newObjectsReadsJournal(mod.db.UpdateReadAt, log)

	mod.setupDefaultRepos()

	err := mod.db.Migrate()
	if err != nil {
		return nil, err
	}

	return mod, nil
}

func (mod *Module) setupDefaultRepos() {
	// device group
	device := NewRepoGroup(mod, "This device", false)
	mod.repos.Set(objects.RepoDevice, device)

	// device sub-groups
	memory := NewRepoGroup(mod, "In-memory repos", false)
	mod.repos.Set(objects.RepoMemory, memory)
	device.Add(objects.RepoMemory)

	local := NewRepoGroup(mod, "Local storage", true)
	mod.repos.Set(objects.RepoLocal, local)
	device.Add(objects.RepoLocal)

	removable := NewRepoGroup(mod, "Removable devices", true)
	mod.repos.Set(objects.RepoRemovable, removable)
	device.Add(objects.RepoRemovable)

	// virtual group
	virtual := NewRepoGroup(mod, "Virtual repositories", true)
	mod.repos.Set(objects.RepoVirtual, virtual)

	// network group
	network := NewRepoGroup(mod, "Network repositories", true)
	mod.repos.Set(objects.RepoNetwork, network)

	main := NewRepoGroup(mod, "World", false)
	main.Add(objects.RepoDevice)
	main.Add(objects.RepoVirtual)
	main.Add(objects.RepoNetwork)

	mod.repos.Set(objects.RepoMain, main)

	mem0 := mem.New("Default memory", mod.config.DefaultMemSize)
	mod.repos.Set("mem0", mem0)
	memory.Add("mem0")

	mod.system = mem.New("System memory", mod.config.DefaultMemSize)
	mod.repos.Set(objects.RepoSystem, mod.system)
	memory.Add(objects.RepoSystem)
}

func init() {
	if err := core.RegisterModule(objects.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
