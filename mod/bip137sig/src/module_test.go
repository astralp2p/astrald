package src

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/bip137sig"
	"github.com/astralp2p/astral-go/api/secp256k1"
)

func TestDeriveKeyBIP32Vector1(t *testing.T) {
	// note: BIP-32 test vector 1, https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki#test-vector-1
	seed, err := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	if err != nil {
		t.Fatalf("decode seed: %v", err)
	}

	tests := []struct {
		path string
		want string
	}{
		{"m", "e8f32e723decf4051aefac8e2c93c9c5b214313817cdb01a1494b917c8436b35"},
		{"m/0'", "edb2e14f9ee77d26dd93b4ecede8d16ed408ce149b6cd80b0715a2d911a0afea"},
		{"m/0h/1", "3c6cb8d0f6a264c91ea8b5030fadaa8e538b020f0a387421a12de9319dc93368"},
		{"m/0'/1/2'/2/1000000000", "471b76e389e528d6de6d816857e012c5455051cad6660850e58372a6c3e6e7c8"},
	}

	mod := &Module{}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			key, err := mod.DeriveKey(bip137sig.Seed(seed), tt.path)
			if err != nil {
				t.Fatalf("DeriveKey err = %v; want nil", err)
			}

			if key.Type != secp256k1.KeyType {
				t.Errorf("DeriveKey type = %q; want %q", key.Type, secp256k1.KeyType)
			}

			if got := hex.EncodeToString(key.Key); got != tt.want {
				t.Errorf("DeriveKey key = %s; want %s", got, tt.want)
			}
		})
	}
}

func TestDeriveKeyErrors(t *testing.T) {
	validSeed := make([]byte, 16)

	tests := []struct {
		name string
		seed []byte
		path string
		want string
	}{
		{"short seed", make([]byte, 8), "m", "seed length must be between 128 and 512 bits"},
		{"non-numeric path element", validSeed, "m/x", `"x"`},
	}

	mod := &Module{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mod.DeriveKey(bip137sig.Seed(tt.seed), tt.path)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("DeriveKey err = %v; want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestGenerateSeed(t *testing.T) {
	seed, err := (&Module{}).GenerateSeed()
	if err != nil {
		t.Fatalf("GenerateSeed err = %v; want nil", err)
	}

	if len(seed) != 64 {
		t.Fatalf("GenerateSeed length = %d bytes; want 64", len(seed))
	}
}
