package tree

import (
	"testing"

	"github.com/cryptopunkscc/astral-go/astral"
)

// TestValue_Clear covers the unbound clear path: Clear resets the cached value to
// the zero T where Set cannot — a typed-nil pointer slips past Set's any(v) == nil
// guard. Exercised against the local fallback, with no backing node.
func TestValue_Clear(t *testing.T) {
	var v Value[*astral.String8]

	s := astral.String8("active")
	if err := v.Set(nil, &s); err != nil {
		t.Fatalf("set: %v", err)
	}
	if v.Get() == nil {
		t.Fatal("value not set before clear")
	}

	if err := v.Clear(nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if v.Get() != nil {
		t.Errorf("value not cleared: got %v", v.Get())
	}
}
