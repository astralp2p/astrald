package apphost

import (
	"strings"
	"testing"
)

const testCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_"

func TestRandomString_LengthAndCharset(t *testing.T) {
	for _, length := range []int{0, 1, 32, 200} {
		s, err := randomString(length)
		if err != nil {
			t.Fatalf("length %d: %v", length, err)
		}
		if len(s) != length {
			t.Fatalf("length %d: got %d characters", length, len(s))
		}
		for i, r := range s {
			if !strings.ContainsRune(testCharset, r) {
				t.Fatalf("length %d: character %d is %q, outside the charset", length, i, r)
			}
		}
	}
}

func TestRandomString_DoesNotRepeat(t *testing.T) {
	const runs = 500

	seen := make(map[string]struct{}, runs)
	for i := 0; i < runs; i++ {
		s, err := randomString(32)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[s]; dup {
			t.Fatalf("run %d repeated a token: %q", i, s)
		}
		seen[s] = struct{}{}
	}
}

// Every character must be reachable. A generator that discarded too much — a
// rejection bound below the charset size — would leave the tail unreachable.
func TestRandomString_CoversCharset(t *testing.T) {
	s, err := randomString(64 * len(testCharset))
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range testCharset {
		if !strings.ContainsRune(s, r) {
			t.Fatalf("character %q never appeared in %d draws", r, len(s))
		}
	}
}
