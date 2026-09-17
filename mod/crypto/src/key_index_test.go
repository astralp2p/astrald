package crypto

import (
	"errors"
	"testing"

	secp256k1api "github.com/astralp2p/astral-go/api/secp256k1"
	"github.com/astralp2p/astral-go/astral"
	"github.com/astralp2p/astral-go/astral/log"
	secp256k1src "github.com/astralp2p/astrald/mod/secp256k1/src"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newKeyIndexModule(t *testing.T) *Module {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	// why: every pooled connection to ":memory:" opens its own empty database.
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })

	db, err := newDB(gdb)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	mod := &Module{db: db, log: log.New(nil)}
	mod.AddEngine(secp256k1src.Engine{})

	return mod
}

func mustObjectID(t *testing.T, obj astral.Object) *astral.ObjectID {
	t.Helper()

	id, err := astral.ResolveObjectID(obj)
	if err != nil {
		t.Fatalf("resolve object id: %v", err)
	}
	return id
}

func TestIndexPrivateKeyIsIdempotent(t *testing.T) {
	mod := newKeyIndexModule(t)
	key := secp256k1api.New()

	for i := 1; i <= 2; i++ {
		if err := mod.indexPrivateKey(key); err != nil {
			t.Fatalf("indexPrivateKey call %d err = %v; want nil", i, err)
		}
	}
}

func TestIndexedKeyLookups(t *testing.T) {
	mod := newKeyIndexModule(t)
	key := secp256k1api.New()

	if err := mod.indexPrivateKey(key); err != nil {
		t.Fatalf("indexPrivateKey: %v", err)
	}

	keyID := mustObjectID(t, key)
	pubKeyID := mustObjectID(t, secp256k1api.PublicKey(key))
	unrelatedID := mustObjectID(t, astral.NewString8("unrelated"))

	tests := []struct {
		name string
		id   *astral.ObjectID
		want bool
	}{
		{"private key id", keyID, true},
		{"public key id", pubKeyID, true},
		{"unrelated id", unrelatedID, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexed, err := mod.db.isKeyIndexed(tt.id)
			if err != nil || indexed != tt.want {
				t.Fatalf("isKeyIndexed = %v, %v; want %v, nil", indexed, err, tt.want)
			}
		})
	}

	if !mod.HoldObject(keyID) {
		t.Error("HoldObject(private key id) = false; want true")
	}

	if mod.HoldObject(unrelatedID) {
		t.Error("HoldObject(unrelated id) = true; want false")
	}
}

func TestPrivateKeyID(t *testing.T) {
	mod := newKeyIndexModule(t)
	key := secp256k1api.New()

	if err := mod.indexPrivateKey(key); err != nil {
		t.Fatalf("indexPrivateKey: %v", err)
	}

	got, err := mod.PrivateKeyID(secp256k1api.PublicKey(key))
	if err != nil {
		t.Fatalf("PrivateKeyID err = %v; want nil", err)
	}

	if want := mustObjectID(t, key); !got.IsEqual(want) {
		t.Fatalf("PrivateKeyID = %v; want %v", got, want)
	}

	unknown := secp256k1api.PublicKey(secp256k1api.New())
	if _, err := mod.PrivateKeyID(unknown); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("PrivateKeyID(unknown key) err = %v; want %v", err, gorm.ErrRecordNotFound)
	}
}

func TestAddToIndexRejectsNonKey(t *testing.T) {
	mod := newKeyIndexModule(t)

	err := mod.AddToIndex(astral.NewString8("x"))

	var unexpected *astral.ErrUnexpectedObject
	if !errors.As(err, &unexpected) {
		t.Fatalf("AddToIndex(string8) err = %v; want *astral.ErrUnexpectedObject", err)
	}
}
