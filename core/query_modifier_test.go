package core

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astrald/mod/nodes"
)

func newTestModifier() *QueryModifier {
	return &QueryModifier{query: astral.Launch(&astral.Query{})}
}

func newTestIdentity() *astral.Identity {
	return secp256k1.Identity(secp256k1.PublicKey(secp256k1.New()))
}

func relays(t *testing.T, q *QueryModifier) []*astral.Identity {
	t.Helper()
	v, ok := q.Query().Extra.Get(nodes.ExtraRelayVia)
	if !ok {
		t.Fatalf("Extra[%q] is not set", nodes.ExtraRelayVia)
	}
	l, ok := v.([]*astral.Identity)
	if !ok {
		t.Fatalf("Extra[%q] has type %T, want []*astral.Identity", nodes.ExtraRelayVia, v)
	}
	return l
}

func TestQueryModifierAddRelay(t *testing.T) {
	q := newTestModifier()
	idA, idB := newTestIdentity(), newTestIdentity()

	q.AddRelay(idA)
	if got := relays(t, q); len(got) != 1 || got[0] != idA {
		t.Fatalf("after AddRelay(A) relays = %v, want [%v]", got, idA)
	}

	q.AddRelay(idA)
	if got := relays(t, q); len(got) != 1 {
		t.Fatalf("after duplicate AddRelay(A) relays = %v, want [%v]", got, idA)
	}

	q.AddRelay(idB)
	if got := relays(t, q); len(got) != 2 || got[0] != idA || got[1] != idB {
		t.Fatalf("after AddRelay(B) relays = %v, want [%v %v]", got, idA, idB)
	}
}

func TestQueryModifierBlockKeepsFirst(t *testing.T) {
	q := newTestModifier()
	e1, e2 := errors.New("e1"), errors.New("e2")

	q.Block(e1)
	q.Block(e2)

	if got := q.blocked.Get(); got != e1 {
		t.Fatalf("blocked = %v, want %v", got, e1)
	}
}

func TestQueryModifierAttach(t *testing.T) {
	q := newTestModifier()
	nonce := astral.Nonce(7)
	obj := &nonce

	q.Attach(obj)

	got, ok := q.Query().Extra.Get(nodes.ExtraCallerProof)
	if !ok || got != astral.Object(obj) {
		t.Fatalf("Extra[%q] = (%v, %v), want (%v, true)", nodes.ExtraCallerProof, got, ok, obj)
	}
}

func TestQueryModifierSetCallerTarget(t *testing.T) {
	q := newTestModifier()
	idA, idB := newTestIdentity(), newTestIdentity()

	q.SetCaller(idA)
	q.SetTarget(idB)

	if q.Query().Caller != idA {
		t.Errorf("Caller = %v, want %v", q.Query().Caller, idA)
	}
	if q.Query().Target != idB {
		t.Errorf("Target = %v, want %v", q.Query().Target, idB)
	}
}
