package coldcard

import (
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	"github.com/astralp2p/astral-go/lib/routing"
	"github.com/astralp2p/astral-go/sig"
	authmod "github.com/astralp2p/astrald/mod/auth"
	"github.com/astralp2p/astrald/mod/coldcard"
	"github.com/astralp2p/astrald/mod/crypto"
	usermod "github.com/astralp2p/astrald/mod/user"
	"github.com/astralp2p/astrald/resources"
	"sync"
	"time"
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
	config  Config
	node    astral.Node
	log     *log.Logger
	assets  resources.Resources
	router  routing.OpRouter
	db      *DB
	backend DeviceBackend

	devices sig.Map[string, string]

	// why: Scan reads the recorded set, then deletes from it, so two scans
	// interleave into a device recorded by one and forgotten by the other.
	// note: the periodic scan and coldcard.scan both call Scan.
	mu sync.Mutex
}

// Run scans the attached devices once at start and then on ScanInterval until
// ctx ends. A scan that fails is logged and retried on the next tick.
//
// why periodic: the recorded set answers for the devices connected now, and only
// a scan refreshes it, so a device replugged or plugged in after start is noticed
// without a caller asking for coldcard.scan.
// note: a scan in flight delays this return until its ckcc process exits.
func (mod *Module) Run(ctx *astral.Context) error {
	mod.scanAndLog()

	ticker := time.NewTicker(mod.scanInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			mod.scanAndLog()
		}
	}
}

// scanInterval returns the configured interval, or the default when the config
// sets none.
func (mod *Module) scanInterval() time.Duration {
	if mod.config.ScanInterval <= 0 {
		return defaultConfig.ScanInterval
	}

	return mod.config.ScanInterval
}

// scanAndLog scans once and logs a failure, which leaves the loop running.
func (mod *Module) scanAndLog() {
	if err := mod.Scan(); err != nil {
		mod.log.Logv(1, "coldcard scan: %v", err)
	}
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

	serials, err := mod.backend.List()
	if err != nil {
		return err
	}

	var connected = map[string]struct{}{}

	for _, serial := range serials {
		connected[serial] = struct{}{}

		pubKeyHex, err := mod.backend.ReadPubKey(serial, coldcard.BIP44Path)
		if err != nil {
			continue
		}

		mod.devices.Replace(serial, pubKeyHex)
		mod.log.Logv(1, "found coldcard device: %v for key %v", serial, pubKeyHex)
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

// serialForPublicKeyHex returns the serial of the recorded device holding
// keyHex, and whether a recorded device holds it.
func (mod *Module) serialForPublicKeyHex(keyHex string) (serial string, found bool) {
	for serial, key := range mod.devices.Clone() {
		if key == keyHex {
			return serial, true
		}
	}

	return "", false
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
