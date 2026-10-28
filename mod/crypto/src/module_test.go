package crypto

import (
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/auth"
	"github.com/astralp2p/astral-go/api/crypto"
	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
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

// TestSignNamesAnUnheldKey: signing as a key no engine holds returns
// ErrKeyNotHeld, which names the key and still reads as ErrUnsupported.
func TestSignNamesAnUnheldKey(t *testing.T) {
	mod := &Module{}
	key := secp256k1api.PublicKey(secp256k1api.New())
	text, _ := key.MarshalText()

	_, err := mod.Sign(nil, key, &auth.Contract{})
	if !errors.Is(err, &cryptomod.ErrKeyNotHeld{}) {
		t.Fatalf("Sign err = %v; want ErrKeyNotHeld", err)
	}
	if !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Fatalf("Sign err = %v; want it to unwrap to ErrUnsupported", err)
	}
	if !strings.Contains(err.Error(), string(text)) {
		t.Fatalf("Sign err = %q; want it to name %s", err, text)
	}
}
