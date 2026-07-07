package main

import (
	"bytes"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/secp256k1"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/resources"
)

// loadNodeIdentity loads node's identity from resources. Generates a new identity if we don't have one yet.
func loadNodeIdentity(resources resources.Resources) (identity *astral.Identity, err error) {
	var nodeKey *crypto.PrivateKey

	data, err := resources.Read(resNodeKey)
	if err == nil {
		object, _, _ := astral.Decode(bytes.NewReader(data), astral.Canonical())

		var ok bool
		nodeKey, ok = object.(*crypto.PrivateKey)
		if !ok {
			return nil, astral.NewErrUnexpectedObject(object)
		}
	} else {
		nodeKey = secp256k1.New()

		// store node key
		var keyBytes = &bytes.Buffer{}
		_, err = astral.Encode(keyBytes, nodeKey, astral.Canonical())
		if err != nil {
			return nil, err
		}

		err = resources.Write("node_key", keyBytes.Bytes())
		if err != nil {
			return nil, err
		}
	}

	identity = secp256k1.Identity(secp256k1.PublicKey(nodeKey))

	return
}
