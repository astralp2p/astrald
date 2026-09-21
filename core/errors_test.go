package core

import (
	"errors"
	"fmt"
	"testing"
)

// TestErrModuleUnavailableIsMatchesAnyName pins the method's own doc comment.
// The old *ErrModuleUnavailable target matched only pointer targets, while
// errModuleUnavailable returns the value type.
func TestErrModuleUnavailableIsMatchesAnyName(t *testing.T) {
	err := errModuleUnavailable("a")

	if !errors.Is(err, ErrModuleUnavailable{}) {
		t.Errorf("errors.Is(%v, ErrModuleUnavailable{}) = false, want true", err)
	}
	if !errors.Is(err, ErrModuleUnavailable{Name: "b"}) {
		t.Errorf("errors.Is(%v, ErrModuleUnavailable{Name: b}) = false, want true", err)
	}
	if !errors.Is(fmt.Errorf("load: %w", err), ErrModuleUnavailable{}) {
		t.Errorf("errors.Is(wrapped, ErrModuleUnavailable{}) = false, want true")
	}
	if errors.Is(errors.New("other"), ErrModuleUnavailable{}) {
		t.Errorf("errors.Is(unrelated, ErrModuleUnavailable{}) = true, want false")
	}
}
