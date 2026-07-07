package secp256k1

import (
	"crypto/ecdsa"
	"fmt"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"
	modSecp256k1mod "github.com/cryptopunkscc/astrald/mod/secp256k1"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	modSecp256k1 "github.com/cryptopunkscc/astral-go/api/secp256k1"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type Engine struct {
	mod *Module
}

func (e Engine) DerivePublicKey(ctx *astral.Context, key *crypto.PrivateKey) (*crypto.PublicKey, error) {
	return modSecp256k1.PublicKey(key), nil
}

// NewHashSigner only supports secp256k1 keys with the asn1 scheme.
func (e Engine) NewHashSigner(key *crypto.PublicKey, scheme string) (cryptomod.HashSigner, error) {
	switch {
	case key.Type != modSecp256k1.KeyType:
		return nil, cryptomod.ErrUnsupportedKeyType
	case scheme != "asn1":
		return nil, cryptomod.ErrUnsupportedScheme
	}

	privateKey, err := e.mod.Crypto.PrivateKey(astral.NewContext(nil), key)
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %w", err)
	}

	return modSecp256k1mod.NewHashSignerASN1(privateKey), nil
}

func (e Engine) VerifyHashSignature(key *crypto.PublicKey, sig *crypto.Signature, hash []byte) error {
	switch {
	case key.Type != modSecp256k1.KeyType:
		return cryptomod.ErrUnsupportedKeyType
	case sig.Scheme != "asn1":
		return cryptomod.ErrUnsupportedScheme
	}

	publicKey, err := secp256k1.ParsePubKey(key.Key)
	if err != nil {
		return fmt.Errorf("%w: %w", cryptomod.ErrInvalidSignature, err)
	}

	if ecdsa.VerifyASN1(publicKey.ToECDSA(), hash, sig.Data) {
		return nil
	}

	return cryptomod.ErrInvalidSignature
}
