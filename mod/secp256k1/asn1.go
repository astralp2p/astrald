package secp256k1

import (
	"crypto/ecdsa"
	"crypto/rand"
	"fmt"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// SignASN1 produces an ASN.1-encoded ECDSA signature over hash.
// Rejects keys whose type is not KeyType.
func SignASN1(key *crypto.PrivateKey, hash []byte) (*crypto.Signature, error) {
	switch {
	case key.Type != KeyType:
		return nil, cryptomod.ErrUnsupportedKeyType
	}

	sig, err := ecdsa.SignASN1(
		rand.Reader,
		secp256k1.PrivKeyFromBytes(key.Key).ToECDSA(),
		hash,
	)
	if err != nil {
		return nil, err
	}

	return &crypto.Signature{
		Scheme: "asn1",
		Data:   sig,
	}, nil
}

// VerifyASN1 checks an ASN.1-encoded ECDSA signature over hash.
// Accepts only KeyType keys and the "asn1" scheme; returns ErrInvalidSignature on mismatch.
func VerifyASN1(key *crypto.PublicKey, hash []byte, sig *crypto.Signature) error {
	switch {
	case key.Type != KeyType:
		return cryptomod.ErrUnsupportedKeyType
	case sig.Scheme != "asn1":
		return cryptomod.ErrUnsupportedScheme
	}

	// parse the key
	pkey, err := secp256k1.ParsePubKey(key.Key)
	if err != nil {
		return fmt.Errorf("%w: %w", cryptomod.ErrInvalidSignature, err)
	}

	// verify sig
	if ecdsa.VerifyASN1(pkey.ToECDSA(), hash, sig.Data) {
		return nil
	}

	return cryptomod.ErrInvalidSignature
}
