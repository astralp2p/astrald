package coldcard

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/coldcard"
	"github.com/astralp2p/astrald/mod/coldcard/ckcc"
	"github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/resources"
	"sync"
)

type Deps struct {
	Auth   authmod.Module
	Crypto crypto.Module
}

// OptionalDeps holds the user module, which AuthorizeScanAction reads to name
// the swarm's user.
//
// why optional: a node that loads no user module still scans as itself, and the
// setup ceremony scans before a user claims the node.
type OptionalDeps struct {
	User usermod.Module
}

type Module struct {
	Deps
	OptionalDeps
	config Config
	node   astral.Node
	log    *log.Logger
	assets resources.Resources
	router routing.OpRouter
	db     *DB

	devices sig.Map[string, string]

	// why: Scan reads the recorded set, then deletes from it, so two scans
	// interleave into a device recorded by one and forgotten by the other.
	mu sync.Mutex
}

func (mod *Module) Run(ctx *astral.Context) error {
	go mod.Scan()
	<-ctx.Done()
	return nil
}

// Scan enumerates connected ColdCards and records each serial to pubkey,
// replacing the pubkey recorded for a serial that is still connected. A serial
// the enumeration no longer lists is forgotten, so an unplugged device stops
// being offered as a signer (Engine.NewTextSigner).
//
// note: a device whose pubkey cannot be read is skipped and keeps what was
// recorded for it, because the enumeration still lists it as connected.
// why: a failed enumeration returns the error and leaves the recorded set
// untouched, so a transient ckcc failure does not un-register every signer.
func (mod *Module) Scan() error {
	mod.mu.Lock()
	defer mod.mu.Unlock()

	devices, err := ckcc.List()
	if err != nil {
		return err
	}

	var connected = map[string]struct{}{}

	for _, dev := range devices {
		connected[dev.Serial] = struct{}{}

		pubKeyHex, err := dev.PubKey(coldcard.BIP44Path)
		if err != nil {
			continue
		}

		mod.devices.Replace(dev.Serial, pubKeyHex)
		mod.log.Logv(1, "found coldcard device: %v for key %v", dev.Serial, pubKeyHex)
	}

	for serial := range mod.devices.Clone() {
		if _, found := connected[serial]; found {
			continue
		}

		mod.devices.Delete(serial)
		mod.log.Logv(1, "coldcard device gone: %v", serial)
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
