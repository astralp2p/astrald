package user

import (
	"bytes"
	"testing"
	"time"

	"github.com/cryptopunkscc/astral-go/api/crypto"
	"github.com/cryptopunkscc/astral-go/astral"
)

// timeWithNanos is a fixed instant carrying sub-second precision, used to prove
// ExpelledAt survives encode/decode and the DB column without losing nanoseconds
// (astral.Time encodes as UnixNano, so the signature commits to that precision).
func timeWithNanos() time.Time {
	return time.Unix(1700000000, 123456789).UTC()
}

// sampleSignedExpulsion builds a SignedExpulsion with a dummy (non-cryptographic)
// signature, sufficient for exercising encode/decode and hashing.
func sampleSignedExpulsion() *SignedExpulsion {
	return &SignedExpulsion{
		Expulsion: &Expulsion{
			Issuer:     astral.GenerateIdentity(),
			Subject:    astral.GenerateIdentity(),
			ExpelledAt: astral.Time(timeWithNanos()),
		},
		IssuerSig: &crypto.Signature{
			Scheme: crypto.SchemeASN1,
			Data:   astral.Bytes16("dummy-signature-bytes"),
		},
	}
}

func TestExpulsion_BinaryRoundTrip(t *testing.T) {
	orig := sampleSignedExpulsion()

	data, err := astral.EncodeBytes(orig)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	got, err := astral.DecodeAs[*SignedExpulsion](data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	assertSignedEqual(t, orig, got)
}

func TestExpulsion_JSONRoundTrip(t *testing.T) {
	orig := sampleSignedExpulsion()

	data, err := orig.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got SignedExpulsion
	if err := got.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	assertSignedEqual(t, orig, &got)
}

// TestExpulsion_Registered confirms both objects are registered with astral so
// they can be decoded by type from the wire.
func TestExpulsion_Registered(t *testing.T) {
	for _, typ := range []string{"mod.user.expulsion", "mod.user.signed_expulsion"} {
		if obj := astral.New(typ); obj == nil {
			t.Fatalf("type %q not registered", typ)
		}
	}
}

func TestSignedExpulsion_IsNil(t *testing.T) {
	var nilPtr *SignedExpulsion
	if !nilPtr.IsNil() {
		t.Fatal("nil receiver should report IsNil")
	}
	if !(&SignedExpulsion{}).IsNil() {
		t.Fatal("embedded nil *Expulsion should report IsNil")
	}
	if sampleSignedExpulsion().IsNil() {
		t.Fatal("fully populated value should not report IsNil")
	}
}

func assertSignedEqual(t *testing.T, want, got *SignedExpulsion) {
	t.Helper()

	if !got.Issuer.IsEqual(want.Issuer) {
		t.Errorf("issuer mismatch: got %v want %v", got.Issuer, want.Issuer)
	}
	if !got.Subject.IsEqual(want.Subject) {
		t.Errorf("subject mismatch: got %v want %v", got.Subject, want.Subject)
	}
	if !got.ExpelledAt.Time().Equal(want.ExpelledAt.Time()) {
		t.Errorf("expelledAt mismatch: got %v want %v", got.ExpelledAt.Time(), want.ExpelledAt.Time())
	}
	if got.IssuerSig.Scheme != want.IssuerSig.Scheme {
		t.Errorf("sig scheme mismatch: got %v want %v", got.IssuerSig.Scheme, want.IssuerSig.Scheme)
	}
	if !bytes.Equal(got.IssuerSig.Data, want.IssuerSig.Data) {
		t.Errorf("sig data mismatch")
	}
	// The signature commits to SignableHash, so a stable hash across the round
	// trip is what guarantees the rebuilt object still verifies.
	if !bytes.Equal(got.SignableHash(), want.SignableHash()) {
		t.Errorf("signable hash changed across round trip")
	}
}
