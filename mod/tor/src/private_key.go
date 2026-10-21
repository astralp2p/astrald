package tor

import (
	"context"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/astral/log"
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
	return mod.assets.ReadData(privateKeyFileName)
}

func (mod *Module) savePrivateKey(key Key) error {
	return mod.assets.WriteData(privateKeyFileName, key)
}

// moveKeyToDataRoot moves a tor.key that an earlier version wrote to the config root into the data root.
// A copy left in the config root is a warning; the data-root key is in use.
//
// why: the key fixes the onion address, so it moves before the server reads the
// data root and generates a new key there.
// note: memory resources have one namespace, so there is nothing to move
func moveKeyToDataRoot(res resources.Resources, log *log.Logger) error {
	files, ok := res.(*resources.FileResources)
	if !ok {
		return nil
	}

	err := files.MoveToDataRoot(privateKeyFileName)
	if errors.Is(err, resources.ErrRootCopyKept) {
		log.Log("warning: %v", err)
		return nil
	}

	return err
}

func (mod *Module) generatePrivateKey() (Key, error) {
	service, err := mod.torServer.listen(context.Background(), nil)
	if err != nil {
		return nil, err
	}

	defer service.Close()

	return service.PrivateKey(), err
}
