package src

import (
	"encoding/base64"
	"fmt"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/cryptopunkscc/bip-0137/verify"
	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
)

type Engine struct {
	mod *Module
}

// NewTextSigner returns a BIP-137 signer for the key.
// Only the BIP137 scheme and secp256k1 key type are supported.
func (e Engine) NewTextSigner(key *crypto.PublicKey, scheme string) (cryptomod.TextSigner, error) {
	switch {
	case scheme != crypto.SchemeBIP137:
		return nil, cryptomod.ErrUnsupportedScheme
	case key.Type != secp256k1.KeyType:
		return nil, cryptomod.ErrUnsupportedKeyType
	}

	privateKey, err := e.mod.Crypto.PrivateKey(astral.NewContext(nil), key)
	if err != nil {
		return nil, fmt.Errorf("failed to get private key: %w", err)
	}

	compressed, err := isCompressedPublicKey(key.Key)
	if err != nil {
		return nil, err
	}

	privKey, _ := btcec.PrivKeyFromBytes(privateKey.Key)
	return &MessageSigner{
		key:        privKey,
		compressed: compressed,
	}, nil

}

func (e Engine) VerifyTextSignature(key *crypto.PublicKey, sig *crypto.Signature, msg string) error {
	switch {
	case key.Type != secp256k1.KeyType:
		return cryptomod.ErrUnsupportedKeyType
	case sig.Scheme != crypto.SchemeBIP137:
		return cryptomod.ErrUnsupportedScheme
	}

	publicKey, err := secp.ParsePubKey(key.Key)
	if err != nil {
		return fmt.Errorf("%w: %w", cryptomod.ErrInvalidSignature, err)
	}

	sigBase64 := base64.StdEncoding.EncodeToString(sig.Data)

	ok, err := verify.VerifyWithPubKey(publicKey, msg, sigBase64)
	if err != nil {
		return fmt.Errorf("%w: %w", cryptomod.ErrInvalidSignature, err)
	}
	if !ok {
		return cryptomod.ErrInvalidSignature
	}

	return nil
}

func isCompressedPublicKey(key []byte) (bool, error) {
	switch len(key) {
	case 33:
		return true, nil
	case 65:
		return false, nil
	default:
		return false, fmt.Errorf("invalid public key length: %d", len(key))
	}
}
