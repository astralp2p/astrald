package dir

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func TestIsValidDomain(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"a.b.co", true},
		{"xn--d1acj3b.com", true},
		{"localhost", false},
		{"alice", false},
		{"", false},
		{"-bad.com", false},
		{"a_b.com", false},
		{"a.c", false},
		{"example.com.", false},
	}

	for _, tt := range tests {
		if got := isValidDomain(tt.domain); got != tt.want {
			t.Errorf("isValidDomain(%q) = %v; want %v", tt.domain, got, tt.want)
		}
	}
}

func TestDNSResolveIdentityRefusesPlainName(t *testing.T) {
	// note: the zero DNS has a nil module, so a failed lookup would panic in its error log.
	got, err := DNS{}.ResolveIdentity("alice")

	if err == nil || err.Error() != "cannot resolve" {
		t.Fatalf("ResolveIdentity(alice) err = %v; want %q", err, "cannot resolve")
	}

	if !got.IsZero() {
		t.Fatalf("ResolveIdentity(alice) = %v; want the zero identity", got)
	}
}

func TestDNSDisplayNameIsEmpty(t *testing.T) {
	for _, id := range []*astral.Identity{nil, astral.GenerateIdentity()} {
		if got := (DNS{}).DisplayName(id); got != "" {
			t.Errorf("DisplayName(%v) = %q; want empty", id, got)
		}
	}
}
