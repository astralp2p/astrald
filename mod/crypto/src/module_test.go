package crypto

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	secp256k1 "github.com/astralp2p/astrald/mod/secp256k1/src"
)

// TestIndexPrivateKeyRefusesUnsupportedKeyType: indexing a private key of a
// type no engine supports returns an error.
//
// why: the secp256k1 engine answered such a key with a nil public key and a
// nil error. indexRepo runs in a goroutine with no recover, so the nil
// dereference that followed stopped the node.
func TestIndexPrivateKeyRefusesUnsupportedKeyType(t *testing.T) {
	mod := &Module{}
	mod.AddEngine(secp256k1.Engine{})

	err := mod.indexPrivateKey(&crypto.PrivateKey{Type: "ed25519", Key: []byte{1, 2, 3}})
	if !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("indexPrivateKey returned %v; want %v", err, cryptomod.ErrUnsupported)
	}
}
