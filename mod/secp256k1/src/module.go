package secp256k1

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astrald/core/assets"
	"github.com/astralp2p/astrald/mod/crypto"
	"github.com/astralp2p/astrald/mod/secp256k1"
)

type Deps struct {
	Crypto crypto.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets assets.Assets
	router routing.OpRouter
	db     *DB
}

func (mod *Module) Run(ctx *astral.Context) error {
	<-ctx.Done()
	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) CryptoEngine() crypto.Engine {
	return &Engine{mod: mod}
}

func (mod *Module) String() string {
	return secp256k1.ModuleName
}
