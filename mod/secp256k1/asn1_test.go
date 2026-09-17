package secp256k1

import (
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

func hashOf(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func TestSignVerifyASN1RoundTrip(t *testing.T) {
	key := secp256k1api.New()
	pub := secp256k1api.PublicKey(key)
	hash := hashOf("x")

	sig, err := SignASN1(key, hash)
	if err != nil {
		t.Fatalf("SignASN1 err = %v; want nil", err)
	}

	if sig.Scheme != crypto.SchemeASN1 {
		t.Fatalf("SignASN1 scheme = %q; want %q", sig.Scheme, crypto.SchemeASN1)
	}

	if err := VerifyASN1(pub, hash, sig); err != nil {
		t.Fatalf("VerifyASN1 of a fresh signature err = %v; want nil", err)
	}

	if err := VerifyASN1(pub, hashOf("y"), sig); !errors.Is(err, cryptomod.ErrInvalidSignature) {
		t.Fatalf("VerifyASN1 over another hash err = %v; want %v", err, cryptomod.ErrInvalidSignature)
	}
}

func TestHashSignerASN1SignsVerifiableHash(t *testing.T) {
	key := secp256k1api.New()
	hash := hashOf("x")

	sig, err := NewHashSignerASN1(key).SignHash(nil, hash)
	if err != nil {
		t.Fatalf("SignHash err = %v; want nil", err)
	}

	if err := VerifyASN1(secp256k1api.PublicKey(key), hash, sig); err != nil {
		t.Fatalf("VerifyASN1 of SignHash output err = %v; want nil", err)
	}
}

func TestSignASN1RejectsForeignKeyType(t *testing.T) {
	_, err := SignASN1(&crypto.PrivateKey{Type: "ed25519"}, hashOf("x"))
	if !errors.Is(err, cryptomod.ErrUnsupportedKeyType) {
		t.Fatalf("SignASN1(ed25519) err = %v; want %v", err, cryptomod.ErrUnsupportedKeyType)
	}
}

func TestVerifyASN1Errors(t *testing.T) {
	key := secp256k1api.New()
	pub := secp256k1api.PublicKey(key)
	hash := hashOf("x")

	sig, err := SignASN1(key, hash)
	if err != nil {
		t.Fatalf("SignASN1: %v", err)
	}

	tests := []struct {
		name string
		key  *crypto.PublicKey
		sig  *crypto.Signature
		want error
	}{
		{"foreign key type", &crypto.PublicKey{Type: "ed25519", Key: pub.Key}, sig, cryptomod.ErrUnsupportedKeyType},
		{"bip137 scheme", pub, &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: sig.Data}, cryptomod.ErrUnsupportedScheme},
		{"malformed public key", &crypto.PublicKey{Type: secp256k1api.KeyType, Key: []byte{9}}, sig, cryptomod.ErrInvalidSignature},
		{"malformed signature data", pub, &crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1, 2}}, cryptomod.ErrInvalidSignature},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifyASN1(tt.key, hash, tt.sig); !errors.Is(err, tt.want) {
				t.Fatalf("VerifyASN1 err = %v; want %v", err, tt.want)
			}
		})
	}
}
