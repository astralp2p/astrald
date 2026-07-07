package user

import (
	"bytes"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/api/user"
	"github.com/cryptopunkscc/astral-go/astral"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testDB(t *testing.T) *DB {
	t.Helper()

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	db := &DB{DB: gdb}
	if err := db.AutoMigrate(&dbExpulsion{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func sampleSigned(issuer, subject *astral.Identity) *user.SignedExpulsion {
	return &user.SignedExpulsion{
		Expulsion: &user.Expulsion{
			Issuer:  issuer,
			Subject: subject,
			// sub-second precision so the test catches a column that truncates it
			ExpelledAt: astral.Time(time.Unix(1700000000, 123456789).UTC()),
		},
		IssuerSig: &crypto.Signature{
			Scheme: crypto.SchemeASN1,
			Data:   astral.Bytes16("dummy-signature-bytes"),
		},
	}
}

// TestStoreExpulsion_RoundTrip confirms a stored ban rebuilds with its
// signature-committed fields intact — in particular ExpelledAt at nanosecond
// precision, which the issuer signature commits to and a peer re-verifies.
func TestStoreExpulsion_RoundTrip(t *testing.T) {
	db := testDB(t)
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()
	orig := sampleSigned(issuer, subject)

	if err := db.StoreExpulsion(orig); err != nil {
		t.Fatalf("store: %v", err)
	}

	list, err := db.Expulsions(issuer)
	if err != nil {
		t.Fatalf("expulsions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 expulsion, got %d", len(list))
	}

	got := list[0]
	if !got.Issuer.IsEqual(issuer) || !got.Subject.IsEqual(subject) {
		t.Errorf("identity mismatch: issuer=%v subject=%v", got.Issuer, got.Subject)
	}
	if !got.ExpelledAt.Time().Equal(orig.ExpelledAt.Time()) {
		t.Errorf("expelledAt not preserved: got %v want %v", got.ExpelledAt.Time(), orig.ExpelledAt.Time())
	}
	if got.IssuerSig.Scheme != orig.IssuerSig.Scheme || !bytes.Equal(got.IssuerSig.Data, orig.IssuerSig.Data) {
		t.Errorf("signature not preserved")
	}
	// Equal signable hashes mean a peer verifying the rebuilt object against the
	// stored signature would still succeed.
	if !bytes.Equal(got.SignableHash(), orig.SignableHash()) {
		t.Errorf("signable hash changed across DB round trip — signature would fail to verify")
	}
}

// TestStoreExpulsion_Idempotent confirms re-storing the same (issuer, subject)
// pair is a no-op, matching the append-only, irreversible ban semantics.
func TestStoreExpulsion_Idempotent(t *testing.T) {
	db := testDB(t)
	issuer, subject := astral.GenerateIdentity(), astral.GenerateIdentity()

	for i := 0; i < 2; i++ {
		if err := db.StoreExpulsion(sampleSigned(issuer, subject)); err != nil {
			t.Fatalf("store #%d: %v", i, err)
		}
	}

	list, err := db.Expulsions(issuer)
	if err != nil {
		t.Fatalf("expulsions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 row after duplicate store, got %d", len(list))
	}
}

// TestExpelledSubjects returns exactly the banned subjects for an issuer.
func TestExpelledSubjects(t *testing.T) {
	db := testDB(t)
	issuer := astral.GenerateIdentity()
	s1, s2 := astral.GenerateIdentity(), astral.GenerateIdentity()

	if err := db.StoreExpulsion(sampleSigned(issuer, s1)); err != nil {
		t.Fatalf("store s1: %v", err)
	}
	if err := db.StoreExpulsion(sampleSigned(issuer, s2)); err != nil {
		t.Fatalf("store s2: %v", err)
	}

	subjects, err := db.ExpelledSubjects(issuer)
	if err != nil {
		t.Fatalf("expelledSubjects: %v", err)
	}
	if len(subjects) != 2 {
		t.Fatalf("expected 2 banned subjects, got %d", len(subjects))
	}

	found := map[string]bool{}
	for _, s := range subjects {
		found[s.String()] = true
	}
	if !found[s1.String()] || !found[s2.String()] {
		t.Errorf("missing expected subjects in %v", subjects)
	}

	// A different issuer has no bans.
	other, err := db.ExpelledSubjects(astral.GenerateIdentity())
	if err != nil {
		t.Fatalf("expelledSubjects(other): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("expected no bans for unrelated issuer, got %d", len(other))
	}
}
