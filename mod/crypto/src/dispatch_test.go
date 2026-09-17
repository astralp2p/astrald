package crypto

import (
	"errors"
	"fmt"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

type fakeHashVerifier struct {
	name  string
	err   error
	calls int
}

func (v *fakeHashVerifier) VerifyHashSignature(*crypto.PublicKey, *crypto.Signature, []byte) error {
	v.calls++
	return v.err
}

func verifyWith(v cryptomod.HashVerifier) error {
	return v.VerifyHashSignature(nil, nil, nil)
}

func nameIfVerified(v cryptomod.HashVerifier) (string, error) {
	if err := verifyWith(v); err != nil {
		return "", err
	}
	return v.(*fakeHashVerifier).name, nil
}

func TestDispatchResultReturnsFirstSuccess(t *testing.T) {
	failing := &fakeHashVerifier{name: "failing", err: errors.New("no")}
	ok := &fakeHashVerifier{name: "ok"}

	got, err := dispatchResult[cryptomod.HashVerifier, string]([]any{"not an engine", failing, ok}, nameIfVerified)
	if err != nil || got != "ok" {
		t.Fatalf("dispatchResult = %q, %v; want %q, nil", got, err, "ok")
	}

	if failing.calls != 1 {
		t.Fatalf("erroring engine called %d times; want 1", failing.calls)
	}
}

func TestDispatchResultWithoutSuccessIsUnsupported(t *testing.T) {
	tests := map[string][]any{
		"all engines error": {
			&fakeHashVerifier{name: "a", err: errors.New("no")},
			&fakeHashVerifier{name: "b", err: cryptomod.ErrInvalidSignature},
		},
		"no engine implements the capability": {"not an engine", 42},
		"no engines":                          nil,
	}

	for name, engines := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := dispatchResult[cryptomod.HashVerifier, string](engines, nameIfVerified)
			if got != "" || !errors.Is(err, cryptomod.ErrUnsupported) {
				t.Fatalf("dispatchResult = %q, %v; want \"\", %v", got, err, cryptomod.ErrUnsupported)
			}
		})
	}
}

func TestDispatchVerifySkipsOtherErrors(t *testing.T) {
	engines := []any{
		&fakeHashVerifier{err: errors.New("other")},
		&fakeHashVerifier{},
	}

	if err := dispatchVerify[cryptomod.HashVerifier](engines, verifyWith); err != nil {
		t.Fatalf("dispatchVerify err = %v; want nil", err)
	}
}

func TestDispatchVerifyStopsOnInvalidSignature(t *testing.T) {
	invalid := fmt.Errorf("%w: x", cryptomod.ErrInvalidSignature)
	ok := &fakeHashVerifier{}
	engines := []any{&fakeHashVerifier{err: invalid}, ok}

	err := dispatchVerify[cryptomod.HashVerifier](engines, verifyWith)
	if err != invalid {
		t.Fatalf("dispatchVerify err = %v; want the engine's error %v", err, invalid)
	}

	if ok.calls != 0 {
		t.Fatalf("engine after an invalid signature called %d times; want 0", ok.calls)
	}
}

func TestDispatchVerifyWithoutMatchIsUnsupported(t *testing.T) {
	tests := map[string][]any{
		"only other errors": {&fakeHashVerifier{err: errors.New("other")}},
		"no engines":        {},
	}

	for name, engines := range tests {
		t.Run(name, func(t *testing.T) {
			if err := dispatchVerify[cryptomod.HashVerifier](engines, verifyWith); !errors.Is(err, cryptomod.ErrUnsupported) {
				t.Fatalf("dispatchVerify err = %v; want %v", err, cryptomod.ErrUnsupported)
			}
		})
	}
}
