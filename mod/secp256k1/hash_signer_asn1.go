package secp256k1

import (
	"crypto/ecdsa"
	"crypto/rand"
	cryptomod "github.com/cryptopunkscc/astrald/mod/crypto"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// HashSignerASN1 is a crypto.HashSigner that emits ASN.1-encoded ECDSA signatures.
// Holds the decoded private key for repeated signing.
type HashSignerASN1 struct {
	key *ecdsa.PrivateKey
}

var _ cryptomod.HashSigner = &HashSignerASN1{}

func NewHashSignerASN1(key *crypto.PrivateKey) *HashSignerASN1 {
	signer := &HashSignerASN1{
		key: secp256k1.PrivKeyFromBytes(key.Key).ToECDSA(),
	}

	return signer
}

func (h *HashSignerASN1) SignHash(ctx *astral.Context, hash []byte) (*crypto.Signature, error) {
	sig, err := ecdsa.SignASN1(rand.Reader, h.key, hash)
	if err != nil {
		return nil, err
	}

	return &crypto.Signature{
		Scheme: "asn1",
		Data:   sig,
	}, nil
}
