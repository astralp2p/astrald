package apphost

import (
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/lib/query"
)

func TestEnRouteQueryExtrasUnknownNonce(t *testing.T) {
	mod := &Module{}

	if got := mod.EnRouteQueryExtras(astral.NewNonce()); got != nil {
		t.Fatalf("EnRouteQueryExtras(unknown) = %v, want nil", got)
	}
}

func TestEnRouteQueryExtrasReturnsACopy(t *testing.T) {
	mod := &Module{}
	caller := astral.GenerateIdentity()
	q := astral.Launch(query.New(caller, caller, "test.pending", nil))
	q.Extra.Set("k", "v")
	mod.enRoute.Set(q.Nonce, &queryEnRoute{query: q, cancel: func(error) {}})

	got := mod.EnRouteQueryExtras(q.Nonce)
	if got["k"] != "v" {
		t.Fatalf("extras[k] = %v, want v", got["k"])
	}

	got["k"] = "changed"
	got["added"] = true

	if v, _ := q.Extra.Get("k"); v != "v" {
		t.Fatalf("query Extra[k] = %v after mutating the copy, want v", v)
	}
	if _, ok := q.Extra.Get("added"); ok {
		t.Fatal("a key added to the copy reached the query Extra")
	}
}
