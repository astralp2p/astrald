package crypto

import "github.com/cryptopunkscc/astral-go/astral"

// SignableObject is a base interface for all contracts
type SignableObject interface {
	astral.Object

	// SignableHash returns the hash of the signable object
	SignableHash() []byte
}

// SignableTextObject is an interface for contracts that can be signed as a text message
type SignableTextObject interface {
	SignableObject

	// SignableText returns a human-readable text of the contract no longer than 200 characters.
	SignableText() string
}
