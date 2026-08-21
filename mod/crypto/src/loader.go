package crypto

import (
	"bytes"
	"errors"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astrald/core"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/resources"
)

type Loader struct{}

func (Loader) Load(node astral.Node, assets assets.Assets, log *log.Logger) (core.Module, error) {
	var err error
	var mod = &Module{
		config: defaultConfig,
		node:   node,
		log:    log,
		assets: assets,
	}

	_ = assets.LoadYAML(cryptomod.ModuleName, &mod.config)

	mod.router.AddStructPrefix(mod, "Op")

	mod.db, err = newDB(mod.assets.Database())
	if err != nil {
		return nil, err
	}

	mod.nodeKey, err = mod.loadNodeKey(assets.Res())
	if err != nil {
		return nil, err
	}

	return mod, err
}

// loadNodeKey loads node's private key
func (mod *Module) loadNodeKey(res resources.Resources) (*crypto.PrivateKey, error) {
	keyBytes, err := res.Read("node_key")
	if err != nil {
		return nil, err
	}

	object, _, err := astral.Decode(bytes.NewReader(keyBytes), astral.Canonical())
	if err != nil {
		return nil, err
	}

	privKey, ok := object.(*crypto.PrivateKey)
	if !ok {
		return nil, errors.New("invalid node key")
	}

	return privKey, nil
}

func init() {
	if err := core.RegisterModule(cryptomod.ModuleName, Loader{}); err != nil {
		panic(err)
	}
}
