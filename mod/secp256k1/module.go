/*
Package secp256k1 contains a module that adds secp256k1 support to the crypto module.

It supports secp256k1 keys, asn1 hash signing, and bip137 message signing.
*/
package secp256k1

import (
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/cryptopunkscc/astrald/mod/crypto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const ModuleName = "secp256k1"
const KeyType = "secp256k1"
const MethodNew = "secp256k1.new"

type Module interface {
}

// New generates a new private key
func New() *crypto.PrivateKey {
	pkey, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return nil
	}

	return &crypto.PrivateKey{
		Type: KeyType,
		Key:  pkey.Serialize(),
	}
}

// PublicKey returns the public key of the given private key
func PublicKey(key *crypto.PrivateKey) *crypto.PublicKey {
	if key.Type != KeyType {
		return nil
	}

	return &crypto.PublicKey{
		Type: KeyType,
		Key:  secp256k1.PrivKeyFromBytes(key.Key).PubKey().SerializeCompressed(),
	}
}

// FromIdentity returns Identity's public key
func FromIdentity(identity *astral.Identity) *crypto.PublicKey {
	return &crypto.PublicKey{
		Type: KeyType,
		Key:  identity.PublicKey().SerializeCompressed(),
	}
}

// Identity returns Identity from the public key
func Identity(key *crypto.PublicKey) *astral.Identity {
	pubKey, err := secp256k1.ParsePubKey(key.Key)
	if err != nil {
		return nil

	}

	return astral.IdentityFromPubKey(pubKey)
}
