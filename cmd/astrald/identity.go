package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/resources"
)

// loadNodeIdentity loads node's identity from resources. Generates a new identity only if no node key
// exists yet (resources.ErrNotFound); any other read error is returned.
func loadNodeIdentity(res resources.Resources) (identity *astral.Identity, err error) {
	var nodeKey *crypto.PrivateKey

	data, err := res.Read(resNodeKey)
	if err == nil {
		object, _, err := astral.Decode(bytes.NewReader(data), astral.Canonical())
		if err != nil {
			return nil, fmt.Errorf("decode node_key: %w", err)
		}

		var ok bool
		nodeKey, ok = object.(*crypto.PrivateKey)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(object)
		}
	} else if !errors.Is(err, resources.ErrNotFound) {
		return nil, fmt.Errorf("read node_key: %w", err)
	} else {
		nodeKey = secp256k1.New()

		// store node key
		var keyBytes = &bytes.Buffer{}
		_, err = astral.Encode(keyBytes, nodeKey, astral.Canonical())
		if err != nil {
			return nil, err
		}

		err = res.Write("node_key", keyBytes.Bytes())
		if err != nil {
			return nil, err
		}
	}

	identity = secp256k1.Identity(secp256k1.PublicKey(nodeKey))

	return
}
