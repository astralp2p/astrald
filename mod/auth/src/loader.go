package auth

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/auth"
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

	_ = assets.LoadYAML(auth.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	mod.Add(auth.Func[*auth.SudoAction](mod.AuthorizeSudo))

	mod.db = &DB{DB: assets.Database()}
	if err = mod.db.AutoMigrate(&dbContract{}, &dbContractPermit{}); err != nil {
		return nil, err
	}

	return mod, err
}

func init() {
	if err := core.RegisterModule(auth.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
