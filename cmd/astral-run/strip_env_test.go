package main

import (
	"slices"
	"testing"
)

// TestStripEnvDoesNotModifyInput pins the doc comment: stripEnv returns a copy.
// Filtering into env[:0:len(env)] reused the caller's backing array and
// overwrote it with the kept entries.
func TestStripEnvDoesNotModifyInput(t *testing.T) {
	env := []string{"TOKEN=x", "A=1"}

	got := stripEnv(env, "TOKEN")

	if want := []string{"A=1"}; !slices.Equal(got, want) {
		t.Fatalf("stripEnv = %q, want %q", got, want)
	}
	if want := []string{"TOKEN=x", "A=1"}; !slices.Equal(env, want) {
		t.Fatalf("stripEnv modified its input: env = %q, want %q", env, want)
	}
}
