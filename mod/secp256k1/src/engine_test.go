package secp256k1

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// note: any cryptomod.Module method other than PrivateKey panics on the nil embedded interface.
type privateKeyStore struct {
	cryptomod.Module
	key *crypto.PrivateKey
	err error
}

func (s *privateKeyStore) PrivateKey(*astral.Context, *crypto.PublicKey) (*crypto.PrivateKey, error) {
	return s.key, s.err
}

func engineWithStore(store *privateKeyStore) Engine {
	return Engine{mod: &Module{Deps: Deps{Crypto: store}}}
}

func TestEngineDerivePublicKey(t *testing.T) {
	key := secp256k1api.New()

	got, err := Engine{}.DerivePublicKey(astral.NewContext(nil), key)
	if err != nil {
		t.Fatalf("DerivePublicKey err = %v; want nil", err)
	}

	// why: DerivePublicKey delegates to secp256k1api.PublicKey, so the expected key comes from the curve library.
	wantKey := secp256k1.PrivKeyFromBytes(key.Key).PubKey().SerializeCompressed()
	if got.Type != secp256k1api.KeyType || !bytes.Equal(got.Key, wantKey) {
		t.Fatalf("DerivePublicKey = type %q, key %x; want type %q, key %x", got.Type, got.Key, secp256k1api.KeyType, wantKey)
	}
}

func TestEngineDerivePublicKeyRejectsForeignKeyType(t *testing.T) {
	got, err := Engine{}.DerivePublicKey(astral.NewContext(nil), &crypto.PrivateKey{Type: "ed25519", Key: []byte{1}})
	if got != nil || !errors.Is(err, cryptomod.ErrUnsupportedKeyType) {
		t.Fatalf("DerivePublicKey(ed25519) = %v, %v; want nil, %v", got, err, cryptomod.ErrUnsupportedKeyType)
	}
}

func TestEngineNewHashSignerGuards(t *testing.T) {
	pub := secp256k1api.PublicKey(secp256k1api.New())

	tests := []struct {
		name   string
		key    *crypto.PublicKey
		scheme string
		want   error
	}{
		{"foreign key type", &crypto.PublicKey{Type: "ed25519", Key: pub.Key}, crypto.SchemeASN1, cryptomod.ErrUnsupportedKeyType},
		{"bip137 scheme", pub, crypto.SchemeBIP137, cryptomod.ErrUnsupportedScheme},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := Engine{}.NewHashSigner(tt.key, tt.scheme)
			if signer != nil || !errors.Is(err, tt.want) {
				t.Fatalf("NewHashSigner = %v, %v; want nil, %v", signer, err, tt.want)
			}
		})
	}
}

func TestEngineNewHashSignerSignsVerifiableHash(t *testing.T) {
	key := secp256k1api.New()
	pub := secp256k1api.PublicKey(key)
	engine := engineWithStore(&privateKeyStore{key: key})
	hash := sha256.Sum256([]byte("x"))

	signer, err := engine.NewHashSigner(pub, crypto.SchemeASN1)
	if err != nil {
		t.Fatalf("NewHashSigner err = %v; want nil", err)
	}

	sig, err := signer.SignHash(astral.NewContext(nil), hash[:])
	if err != nil {
		t.Fatalf("SignHash err = %v; want nil", err)
	}

	if err := engine.VerifyHashSignature(pub, sig, hash[:]); err != nil {
		t.Fatalf("VerifyHashSignature err = %v; want nil", err)
	}
}

func TestEngineNewHashSignerWrapsKeyLookupError(t *testing.T) {
	lookupErr := errors.New("key not found")
	engine := engineWithStore(&privateKeyStore{err: lookupErr})

	_, err := engine.NewHashSigner(secp256k1api.PublicKey(secp256k1api.New()), crypto.SchemeASN1)
	if !errors.Is(err, lookupErr) {
		t.Fatalf("NewHashSigner err = %v; want it to wrap %v", err, lookupErr)
	}

	if !strings.HasPrefix(err.Error(), "failed to get private key") {
		t.Fatalf("NewHashSigner err = %q; want prefix %q", err, "failed to get private key")
	}
}
