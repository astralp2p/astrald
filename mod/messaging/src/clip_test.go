package messaging

import (
	"testing"
	"unicode/utf8"
)

func TestClipCutsOnARuneBoundary(t *testing.T) {
	for _, c := range []struct {
		text string
		n    int
		want string
	}{
		{"hello", 5, "hello"},
		{"hello", 3, "hel… (cut)"},
		{"aé", 2, "a… (cut)"},
		{"é", 1, "… (cut)"},
	} {
		got := clip(c.text, c.n)
		if got != c.want {
			t.Errorf("clip(%q, %d) = %q, want %q", c.text, c.n, got, c.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("clip(%q, %d) = %q, which is not valid UTF-8", c.text, c.n, got)
		}
	}
}
