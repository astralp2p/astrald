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
	return &Module{db: db}
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
