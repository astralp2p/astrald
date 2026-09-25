package apphost

import (
	"errors"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testTokenModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbAccessToken{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Module{Deps: Deps{Dir: &namingDir{}}, db: db}
}

func TestAuthenticateToken(t *testing.T) {
	mod := testTokenModule(t)
	identity := astral.GenerateIdentity()

	token, err := mod.CreateAccessToken(identity, astral.Duration(time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	resolved, err := mod.AuthenticateToken(string(token.Token))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !resolved.IsEqual(identity) {
		t.Fatalf("authenticate resolved %v, want %v", resolved, identity)
	}
}

func TestDeleteAccessToken(t *testing.T) {
	mod := testTokenModule(t)

	token, err := mod.CreateAccessToken(astral.GenerateIdentity(), astral.Duration(time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	if err = mod.DeleteAccessToken(string(token.Token)); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err = mod.AuthenticateToken(string(token.Token)); err == nil {
		t.Fatal("authenticate succeeded after delete")
	}
}

func TestDeleteAccessTokenNotFound(t *testing.T) {
	mod := testTokenModule(t)

	err := mod.DeleteAccessToken("no-such-token")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("delete absent token: got %v, want gorm.ErrRecordNotFound", err)
	}
}

func TestAuthenticateExpiredToken(t *testing.T) {
	mod := testTokenModule(t)

	token, err := mod.CreateAccessToken(astral.GenerateIdentity(), astral.Duration(-time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	if _, err = mod.AuthenticateToken(string(token.Token)); err == nil {
		t.Fatal("authenticate succeeded on an expired token")
	}
}

// Every token issued for the identity goes, expired ones included, and no other
// identity's token does. An identity holding none is not an error.
func TestDeleteAccessTokens(t *testing.T) {
	mod := testTokenModule(t)
	identity, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	var issued []string
	for _, d := range []time.Duration{time.Hour, 2 * time.Hour, -time.Hour} {
		token, err := mod.CreateAccessToken(identity, astral.Duration(d))
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		issued = append(issued, string(token.Token))
	}
	kept, err := mod.CreateAccessToken(other, astral.Duration(time.Hour))
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	if err = mod.DeleteAccessTokens(identity); err != nil {
		t.Fatalf("delete tokens: %v", err)
	}

	list, err := mod.ListAccessTokens()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, token := range list {
		if token.Identity.IsEqual(identity) {
			t.Fatalf("token %v of the identity survived the delete", token.Token)
		}
	}
	for _, token := range issued {
		if _, err = mod.AuthenticateToken(token); err == nil {
			t.Fatalf("token %v still authenticates after the delete", token)
		}
	}
	if _, err = mod.AuthenticateToken(string(kept.Token)); err != nil {
		t.Fatalf("another identity's token stopped authenticating: %v", err)
	}

	if err = mod.DeleteAccessTokens(identity); err != nil {
		t.Fatalf("deleting the tokens of an identity holding none: %v", err)
	}
}
