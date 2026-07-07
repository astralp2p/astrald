package coldcard

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astral-go/astral/log"
	"github.com/cryptopunkscc/astral-go/lib/routing"
	"github.com/cryptopunkscc/astral-go/sig"
	"github.com/cryptopunkscc/astrald/mod/coldcard"
	"github.com/cryptopunkscc/astrald/mod/coldcard/ckcc"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/cryptopunkscc/astrald/resources"
)

type Deps struct {
	Crypto crypto.Module
}

type Module struct {
	Deps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	router routing.OpRouter
	db     *DB

	devices sig.Map[string, string]
}

func (mod *Module) Run(ctx *astral.Context) error {
	go mod.Scan()
	<-ctx.Done()
	return nil
}

// Scan enumerates connected ColdCards and records each serial to pubkey;
// devices whose pubkey cannot be read are skipped.
func (mod *Module) Scan() error {
	devices, err := ckcc.List()
	if err != nil {
		return err
	}

	for _, dev := range devices {
		pubKeyHex, err := dev.PubKey(coldcard.BIP44Path)
		if err != nil {
			continue
		}

		mod.devices.Set(dev.Serial, pubKeyHex)
		mod.log.Logv(1, "found coldcard device: %v for key %v", dev.Serial, pubKeyHex)
	}

	return nil
}

func (mod *Module) deviceForPublicKeyHex(keyHex string) *ckcc.Device {
	for serial, key := range mod.devices.Clone() {
		if key == keyHex {
			return ckcc.NewDevice(serial)
		}
	}

	return nil
}

func (mod *Module) Router() astral.Router {
	return &mod.router
}

func (mod *Module) CryptoEngine() crypto.Engine {
	return &Engine{mod: mod}
}

func (mod *Module) String() string {
	return coldcard.ModuleName
}
