package apphost

import (
	"context"
	"testing"

	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
)

func testPrepareModule(t *testing.T, tokens map[string]string, aliases map[string]*astral.Identity) *Module {
	t.Helper()

	mod := testTokenModule(t)
	mod.log = log.New(nil)
	mod.Dir = &namingDir{aliases: aliases}
	mod.config.Tokens = tokens
	return mod
}

func TestPrepareSeedsAConfiguredToken(t *testing.T) {
	app := astral.GenerateIdentity()
	mod := testPrepareModule(t, map[string]string{"tok": "scout"}, map[string]*astral.Identity{"scout": app})

	if err := mod.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	got, err := mod.AuthenticateToken("tok")
	if err != nil {
		t.Fatalf("AuthenticateToken(tok): %v", err)
	}
	if !got.IsEqual(app) {
		t.Fatalf("AuthenticateToken(tok) = %v, want %v", got, app)
	}
}

func TestPrepareTwiceKeepsOneTokenRow(t *testing.T) {
	app := astral.GenerateIdentity()
	mod := testPrepareModule(t, map[string]string{"tok": "scout"}, map[string]*astral.Identity{"scout": app})

	for i := 1; i <= 2; i++ {
		if err := mod.Prepare(context.Background()); err != nil {
			t.Fatalf("Prepare #%d: %v", i, err)
		}
	}

	tokens, err := mod.ListAccessTokens()
	if err != nil {
		t.Fatalf("ListAccessTokens: %v", err)
	}
	var rows int
	for _, token := range tokens {
		if token.Token == "tok" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("rows for tok = %d, want 1", rows)
	}
}

func TestPrepareSkipsAnUnresolvableIdentity(t *testing.T) {
	mod := testPrepareModule(t, map[string]string{"bad": "nobody"}, nil)

	if err := mod.Prepare(context.Background()); err != nil {
		t.Fatalf("Prepare: %v, want nil", err)
	}

	if id, err := mod.AuthenticateToken("bad"); err == nil {
		t.Fatalf("AuthenticateToken(bad) = %v, want an error", id)
	}
}
