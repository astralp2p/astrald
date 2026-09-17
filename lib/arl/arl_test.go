package arl

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
)

type mapResolver map[string]*astral.Identity

var errNoAlias = errors.New("no alias")

func (r mapResolver) ResolveIdentity(name string) (*astral.Identity, error) {
	if id, ok := r[name]; ok {
		return id, nil
	}
	return nil, errNoAlias
}

func (r mapResolver) DisplayName(id *astral.Identity) string {
	return id.String()
}

func newTestIdentity() *astral.Identity {
	return secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
}

func TestSplit(t *testing.T) {
	tests := []struct {
		in                        string
		caller, target, wantQuery string
	}{
		{in: "alice@bob:op?x=1", caller: "alice", target: "bob", wantQuery: "op?x=1"},
		{in: "bob:op", caller: "", target: "bob", wantQuery: "op"},
		{in: "alice@op", caller: "alice", target: "", wantQuery: "op"},
		{in: "op", caller: "", target: "", wantQuery: "op"},
		{in: "op?id=a:b", caller: "", target: "", wantQuery: "op?id=a:b"},
		{in: "a b@c:d", caller: "", target: "", wantQuery: "a b@c:d"},
		{in: "bob:alice@op", caller: "", target: "bob", wantQuery: "alice@op"},
		{in: "@op", caller: "", target: "", wantQuery: "@op"},
	}

	for _, tt := range tests {
		c, tg, q := Split(tt.in)
		if c != tt.caller || tg != tt.target || q != tt.wantQuery {
			t.Errorf("Split(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.in, c, tg, q, tt.caller, tt.target, tt.wantQuery)
		}
	}
}

func TestParseWithResolver(t *testing.T) {
	idA, idB := newTestIdentity(), newTestIdentity()

	a, err := Parse("astral://alice@bob:op", mapResolver{"alice": idA, "bob": idB})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !a.Caller.IsEqual(idA) {
		t.Errorf("Caller = %v, want %v", a.Caller, idA)
	}
	if !a.Target.IsEqual(idB) {
		t.Errorf("Target = %v, want %v", a.Target, idB)
	}
	if a.Query != "op" {
		t.Errorf("Query = %q, want %q", a.Query, "op")
	}
}

func TestParseResolverError(t *testing.T) {
	_, err := Parse("alice@bob:op", mapResolver{"alice": newTestIdentity()})
	if !errors.Is(err, errNoAlias) {
		t.Fatalf("Parse with unresolvable target error = %v, want %v", err, errNoAlias)
	}
}

func TestParseWithoutResolver(t *testing.T) {
	if _, err := Parse("bad@op", nil); !errors.Is(err, astral.ErrInvalidKeyLength) {
		t.Fatalf("Parse(bad@op) error = %v, want %v", err, astral.ErrInvalidKeyLength)
	}

	a, err := Parse("anyone@op", nil)
	if err != nil {
		t.Fatalf("Parse(anyone@op): %v", err)
	}
	if !a.Caller.IsZero() {
		t.Errorf("Caller = %v, want zero identity", a.Caller)
	}
	if got := a.String(); got != "op" {
		t.Errorf("String() = %q, want %q", got, "op")
	}
}

func TestARLStringRoundTrip(t *testing.T) {
	id := newTestIdentity()

	s := New(id, id, "q").String()
	if want := id.String() + "@" + id.String() + ":q"; s != want {
		t.Fatalf("String() = %q, want %q", s, want)
	}

	a, err := Parse(s, nil)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	if !a.Caller.IsEqual(id) || !a.Target.IsEqual(id) || a.Query != "q" {
		t.Fatalf("Parse(%q) = {%v, %v, %q}, want {%v, %v, %q}", s, a.Caller, a.Target, a.Query, id, id, "q")
	}

	if got := (&ARL{}).String(); got != "" {
		t.Fatalf("empty ARL String() = %q, want empty", got)
	}
}
