package main

import (
	"testing"

	"github.com/astralp2p/astral-go/api/dir"
	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

func newTestIdentity() *astral.Identity {
	return secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
}

func TestResolverWithoutAliasMap(t *testing.T) {
	r := newResolver(nil)
	id := newTestIdentity()

	if got, err := r.ResolveIdentity("x"); err == nil {
		t.Fatalf("ResolveIdentity(x) = (%v, nil), want non-nil error", got)
	}
	if got := r.DisplayName(id); got != id.String() {
		t.Fatalf("DisplayName = %q, want %q", got, id.String())
	}
}

func TestResolverWithAliasMap(t *testing.T) {
	id, other := newTestIdentity(), newTestIdentity()
	r := newResolver(&dir.AliasMap{Aliases: map[string]*astral.Identity{"alice": id}})

	got, err := r.ResolveIdentity("alice")
	if err != nil || !got.IsEqual(id) {
		t.Fatalf("ResolveIdentity(alice) = (%v, %v), want (%v, nil)", got, err, id)
	}
	if name := r.DisplayName(id); name != "alice" {
		t.Fatalf("DisplayName(alice id) = %q, want %q", name, "alice")
	}
	if name := r.DisplayName(other); name != other.String() {
		t.Fatalf("DisplayName(unaliased id) = %q, want %q", name, other.String())
	}
}
