/*
Package crypto provides a module with cryptographic operations and objects.
*/
package crypto

import (
	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

const ModuleName = "crypto"
const DBPrefix = "crypto__"

type Module interface {
	// PrivateKeyID looks up the ObjectID of a private key corresponding to the given public key
	PrivateKeyID(*crypto.PublicKey) (*astral.ObjectID, error)

	// PrivateKey tries to load the private key corresponding to the given public key
	PrivateKey(ctx *astral.Context, key *crypto.PublicKey) (*crypto.PrivateKey, error)

	// DerivePublicKey generates the corresponding public key for the given private key
	DerivePublicKey(ctx *astral.Context, key *crypto.PrivateKey) (*crypto.PublicKey, error)

	// NewHashSigner returns a hash signer for the given public key and scheme
	NewHashSigner(key *crypto.PublicKey, scheme string) (HashSigner, error)

	// VerifyHashSignature verifies the signature of the given hash using the given public key
	VerifyHashSignature(key *crypto.PublicKey, sig *crypto.Signature, hash []byte) error

	// NewTextSigner returns a message signer for the given public key and scheme
	NewTextSigner(key *crypto.PublicKey, scheme string) (TextSigner, error)

	// VerifyTextSignature verifies a text signature
	VerifyTextSignature(signer *crypto.PublicKey, sig *crypto.Signature, text string) error

	// NodeSigner returns a hash signer for the local node
	NodeSigner() HashSigner

	// AddEngine adds a cryptographic engine to the module
	AddEngine(engine Engine)

	// ObjectSigner signs the hash of the given contract with ASN1
	ObjectSigner(*crypto.PublicKey) (ObjectSigner, error)

	// TextObjectSigner signs the text of the given contract with BIP-137
	TextObjectSigner(*crypto.PublicKey) (TextObjectSigner, error)

	VerifyObjectSignature(*crypto.PublicKey, *crypto.Signature, crypto.SignableObject) error

	VerifyTextObjectSignature(*crypto.PublicKey, *crypto.Signature, crypto.SignableTextObject) error

	// Sign signs obj as key, selecting a scheme the key supports: it prefers an
	// object (hash/ASN1) signature and falls back to a text (BIP-137) signature.
	// A convenience over ObjectSigner/TextObjectSigner so callers signing their
	// own wire objects need not repeat the scheme-selection dance.
	Sign(ctx *astral.Context, key *crypto.PublicKey, obj crypto.SignableTextObject) (*crypto.Signature, error)

	// Verify checks sig against obj using key, dispatching on the signature's
	// scheme. It is the counterpart to Sign.
	Verify(key *crypto.PublicKey, sig *crypto.Signature, obj crypto.SignableTextObject) error

	AddToIndex(object astral.Object) error
}

type ObjectSigner interface {
	SignObject(*astral.Context, crypto.SignableObject) (*crypto.Signature, error)
}

type TextObjectSigner interface {
	SignTextObject(*astral.Context, crypto.SignableTextObject) (*crypto.Signature, error)
}
