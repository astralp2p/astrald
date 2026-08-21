package src

import (
	"crypto/sha256"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
)

type MessageSigner struct {
	key        *btcec.PrivateKey
	compressed bool
}

var _ cryptomod.TextSigner = &MessageSigner{}

func (m MessageSigner) SignText(ctx *astral.Context, msg string) (*crypto.Signature, error) {
	hash := hashBitcoinMessage(msg)
	sig := ecdsa.SignCompact(m.key, hash, m.compressed)

	return &crypto.Signature{
		Scheme: crypto.SchemeBIP137,
		Data:   sig,
	}, nil
}

func hashBitcoinMessage(message string) []byte {
	formatted := formatBitcoinMessage(message)

	hash := sha256.Sum256(formatted)
	hash = sha256.Sum256(hash[:])

	return hash[:]
}

func formatBitcoinMessage(message string) []byte {
	prefix := "Bitcoin Signed Message:\n"

	var result []byte

	prefixBytes := []byte(prefix)
	result = appendCompactSize(result, uint64(len(prefixBytes)))
	result = append(result, prefixBytes...)

	messageBytes := []byte(message)
	result = appendCompactSize(result, uint64(len(messageBytes)))
	result = append(result, messageBytes...)

	return result
}

func appendCompactSize(b []byte, n uint64) []byte {
	switch {
	case n < 253:
		return append(b, byte(n))
	case n <= 0xffff:
		return append(b, 253, byte(n), byte(n>>8))
	case n <= 0xffffffff:
		return append(b, 254, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
	default:
		return append(b, 255,
			byte(n),
			byte(n>>8),
			byte(n>>16),
			byte(n>>24),
			byte(n>>32),
			byte(n>>40),
			byte(n>>48),
			byte(n>>56),
		)
	}
}
