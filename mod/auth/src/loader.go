package auth

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

// Load constructs and wires the auth module: reads config, registers the sudo handler,
// opens the DB, and runs AutoMigrate before returning the module to the core runtime.
func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		node:   node,
		log:    log,
		assets: assets,
	}

	if err := assets.LoadYAML(authmod.ModuleName, &mod.config); err != nil && !errors.Is(err, resources.ErrNotFound) {
		return nil, err
	}

	mod.router.AddStructPrefix(mod, "Op")

	mod.Add(authmod.Func[*auth.SudoAction](mod.AuthorizeSudo))

	if err = resolveTokenFiles(mod.config.ExternalAuthorizers); err != nil {
		return nil, err
	}

	// note: the configured authorizers are checked here, so a misconfiguration
	// fails the load; they reach the tree and the registry in LoadDependencies.
	if mod.configured, err = configAssignments(mod.config.ExternalAuthorizers); err != nil {
		return nil, err
	}
	for _, a := range mod.configured {
		if _, err = newAuthorizeAsk(mod, assignmentConfig(a)); err != nil {
			return nil, fmt.Errorf("external authorizer: %w", err)
		}
	}

	mod.db = &DB{DB: assets.Database()}
	if err = mod.db.AutoMigrate(&dbContract{}, &dbContractPermit{}); err != nil {
		return nil, err
	}

	return mod, err
}

func init() {
	if err := core.RegisterModule(authmod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
