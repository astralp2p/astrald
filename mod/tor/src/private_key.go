package tor

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astrald/resources"
)

// Key holds a raw ED25519-V3 private key for a Tor hidden service.
type Key []byte

const privateKeyFileName = "tor.key"

// why: the key fixes the node's onion address, so only a missing key is
// generated. Any other read error is returned and the listener stays down
// instead of advertising a new address.
func (mod *Module) getPrivateKey() (Key, error) {
	key, err := mod.loadPrivateKey()
	switch {
	case err == nil:
		return key, nil
	case !errors.Is(err, resources.ErrNotFound):
		return nil, fmt.Errorf("read %s: %w", privateKeyFileName, err)
	}

	key, err = mod.generatePrivateKey()
	if err != nil {
		return nil, err
	}

	err = mod.savePrivateKey(key)

	return key, err
}

func (mod *Module) loadPrivateKey() (Key, error) {
	return mod.assets.Read(privateKeyFileName)
}

func (mod *Module) savePrivateKey(key Key) error {
	return mod.assets.Write(privateKeyFileName, key)
}

func (mod *Module) generatePrivateKey() (Key, error) {
	service, err := mod.torServer.listen(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	defer service.Close()

	return service.PrivateKey(), err
}
