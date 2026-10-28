package crypto

import (
	"errors"
	"fmt"

	"github.com/astralp2p/astral-go/api/crypto"
)

var (
	ErrUnsupportedKeyType = errors.New("unsupported key type")
	ErrUnsupportedScheme  = errors.New("unsupported scheme")
	ErrInvalidSignature   = errors.New("invalid signature")
	ErrUnsupported        = errors.New("unsupported")

	// ErrForeignKey is returned when a caller asks for a signature under a key
	// that is neither its own identity nor one it may sudo to. A key the node
	// holds is not a key the caller may spend.
	ErrForeignKey = errors.New("cannot sign with another identity's key")

	// ErrNodeKeyNotSignable is returned when a caller arriving as the node asks
	// for a signature under the node's own key. A caller holding sudo for the
	// node still signs; see AuthorizeSigner in the src package for why the
	// caller's identity is not evidence enough here.
	ErrNodeKeyNotSignable = errors.New("cannot sign with the node's key")
)

// ErrKeyNotHeld is returned when no engine signs as Key: the node holds no
// usable private key for it. It unwraps to ErrUnsupported.
type ErrKeyNotHeld struct {
	Key *crypto.PublicKey
}

// Error names the key in its text form and how a key becomes held.
func (e *ErrKeyNotHeld) Error() string {
	text, _ := e.Key.MarshalText()
	return fmt.Sprintf("no signing scheme available for key %s: the node holds no usable private key for it; "+
		"a key is held as an indexed mod.crypto.private_key object or on a connected signing device", text)
}

func (e *ErrKeyNotHeld) Unwrap() error { return ErrUnsupported }

func (e *ErrKeyNotHeld) Is(other error) bool {
	_, ok := other.(*ErrKeyNotHeld)
	return ok
}
