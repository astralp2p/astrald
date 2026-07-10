package crypto

import (
	"errors"

	"github.com/astralp2p/astrald/mod/crypto"
)

// dispatchResult returns the first engine that implements Cap and succeeds.
// Engines lacking Cap, or returning any error, are skipped. If no engine
// produces a result, ErrUnsupported is returned.
func dispatchResult[Cap any, R any](engines []any, call func(Cap) (R, error)) (R, error) {
	var zero R
	for _, e := range engines {
		c, ok := e.(Cap)
		if !ok {
			continue
		}
		if r, err := call(c); err == nil {
			return r, nil
		}
	}
	return zero, crypto.ErrUnsupported
}

// dispatchVerify returns nil on the first engine that verifies the signature.
// ErrInvalidSignature is terminal — it short-circuits and is returned to the
// caller. Any other error means "try next". If no engine matched, returns
// ErrUnsupported.
func dispatchVerify[Cap any](engines []any, call func(Cap) error) error {
	for _, e := range engines {
		c, ok := e.(Cap)
		if !ok {
			continue
		}
		err := call(c)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, crypto.ErrInvalidSignature):
			return err
		}
	}
	return crypto.ErrUnsupported
}
