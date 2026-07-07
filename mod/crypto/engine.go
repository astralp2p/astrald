package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

// Engine is a marker for an opaque cryptographic engine value. An engine is any
// type that implements one or more of the capability interfaces below; the
// crypto module discovers and dispatches per capability via type assertion.
type Engine = any

// PublicKeyDeriver derives the public key from a private key.
type PublicKeyDeriver interface {
	DerivePublicKey(ctx *astral.Context, key *crypto.PrivateKey) (*crypto.PublicKey, error)
}

// HashSignerProvider returns a per-call HashSigner bound to a key + scheme.
type HashSignerProvider interface {
	NewHashSigner(key *crypto.PublicKey, scheme string) (HashSigner, error)
}

// HashVerifier verifies a signature of a hash.
type HashVerifier interface {
	VerifyHashSignature(key *crypto.PublicKey, sig *crypto.Signature, hash []byte) error
}

// TextSignerProvider returns a per-call TextSigner bound to a key + scheme.
type TextSignerProvider interface {
	NewTextSigner(key *crypto.PublicKey, scheme string) (TextSigner, error)
}

// TextVerifier verifies a signature of a text message.
type TextVerifier interface {
	VerifyTextSignature(key *crypto.PublicKey, sig *crypto.Signature, msg string) error
}

type HashSigner interface {
	// SignHash generates a signature for the given hash
	SignHash(ctx *astral.Context, hash []byte) (*crypto.Signature, error)
}

type TextSigner interface {
	// SignText generates a signature for the given text
	SignText(ctx *astral.Context, text string) (*crypto.Signature, error)
}

type EngineProvider interface {
	CryptoEngine() Engine
}
