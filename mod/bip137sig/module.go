package bip137sig

import "github.com/cryptopunkscc/astral-go/api/crypto"
import (
	"github.com/cryptopunkscc/astral-go/api/bip137sig"
)

const ModuleName = "bip137sig"

// Module derives BIP-137 keys from a BIP-39 seed.
type Module interface {
	GenerateSeed() (seed bip137sig.Seed, err error)
	DeriveKey(seed bip137sig.Seed, path string) (privateKey crypto.PrivateKey, err error)
}
