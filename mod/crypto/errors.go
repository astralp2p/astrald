package crypto

import "errors"

var (
	ErrUnsupportedKeyType = errors.New("unsupported key type")
	ErrUnsupportedScheme  = errors.New("unsupported scheme")
	ErrInvalidSignature   = errors.New("invalid signature")
	ErrUnsupported        = errors.New("unsupported")

	// ErrForeignKey is returned when a caller asks for a signature under a key
	// that is not its own identity. A key the node holds is not a key the
	// caller may spend.
	ErrForeignKey = errors.New("cannot sign with another identity's key")

	// ErrNodeKeyNotSignable is returned when a caller asks for a signature
	// under the node's own key. The node's key is never signable through the
	// op surface; see authorizeSigner in the src package for why the caller's
	// identity is not evidence enough here.
	ErrNodeKeyNotSignable = errors.New("cannot sign with the node's key")
)
