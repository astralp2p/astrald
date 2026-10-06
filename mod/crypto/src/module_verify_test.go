package crypto

import (
	"errors"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
)

// note: any astral.Object method panics on the nil embedded interface.
type signableTextObject struct {
	astral.Object
	hash []byte
	text string
}

func (o *signableTextObject) SignableHash() []byte { return o.hash }

func (o *signableTextObject) SignableText() string { return o.text }

func newSignableTextObject() *signableTextObject {
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	return &signableTextObject{hash: hash, text: "hello"}
}

func validVerifyInputs() (*crypto.PublicKey, *crypto.Signature) {
	return &crypto.PublicKey{Type: "secp256k1", Key: []byte{2, 1}},
		&crypto.Signature{Scheme: crypto.SchemeASN1, Data: []byte{1}}
}

func TestVerifyHashSignatureValidatesInputs(t *testing.T) {
	key, sig := validVerifyInputs()
	hash := []byte{1}

	tests := []struct {
		name string
		key  *crypto.PublicKey
		sig  *crypto.Signature
		hash []byte
		want string
	}{
		{"nil key", nil, sig, hash, "public key is nil"},
		{"empty key data", &crypto.PublicKey{Type: key.Type}, sig, hash, "public key data is empty"},
		{"empty key type", &crypto.PublicKey{Key: key.Key}, sig, hash, "public key type is empty"},
		{"nil signature", key, nil, hash, "signature is nil"},
		{"empty signature data", key, &crypto.Signature{Scheme: sig.Scheme}, hash, "signature data is empty"},
		{"empty signature scheme", key, &crypto.Signature{Data: sig.Data}, hash, "signature scheme is empty"},
		{"nil hash", key, sig, nil, "hash is empty"},
	}

	mod := &Module{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mod.VerifyHashSignature(tt.key, tt.sig, tt.hash)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("VerifyHashSignature err = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestVerifyTextSignatureValidatesInputs(t *testing.T) {
	key, sig := validVerifyInputs()
	msg := "hello"

	tests := []struct {
		name string
		key  *crypto.PublicKey
		sig  *crypto.Signature
		msg  string
		want string
	}{
		{"nil key", nil, sig, msg, "public key is nil"},
		{"empty key data", &crypto.PublicKey{Type: key.Type}, sig, msg, "public key data is empty"},
		{"empty key type", &crypto.PublicKey{Key: key.Key}, sig, msg, "public key type is empty"},
		{"nil signature", key, nil, msg, "signature is nil"},
		{"empty signature data", key, &crypto.Signature{Scheme: sig.Scheme}, msg, "signature data is empty"},
		{"empty signature scheme", key, &crypto.Signature{Data: sig.Data}, msg, "signature scheme is empty"},
		{"empty message", key, sig, "", "message is empty"},
	}

	mod := &Module{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mod.VerifyTextSignature(tt.key, tt.sig, tt.msg)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("VerifyTextSignature err = %v; want %q", err, tt.want)
			}
		})
	}
}

func TestVerifyWithoutEnginesIsUnsupported(t *testing.T) {
	mod := &Module{}
	key, sig := validVerifyInputs()

	if err := mod.VerifyHashSignature(key, sig, []byte{1}); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Errorf("VerifyHashSignature err = %v; want %v", err, cryptomod.ErrUnsupported)
	}

	if err := mod.VerifyTextSignature(key, sig, "hello"); !errors.Is(err, cryptomod.ErrUnsupported) {
		t.Errorf("VerifyTextSignature err = %v; want %v", err, cryptomod.ErrUnsupported)
	}
}

func TestVerifyRejectsMissingOrUnknownScheme(t *testing.T) {
	mod := &Module{}
	key, _ := validVerifyInputs()
	obj := newSignableTextObject()

	if err := mod.Verify(key, nil, obj); err == nil || err.Error() != "signature is nil" {
		t.Errorf("Verify(nil signature) err = %v; want %q", err, "signature is nil")
	}

	const want = "unsupported signature scheme: rsa"
	err := mod.Verify(key, &crypto.Signature{Scheme: "rsa", Data: []byte{1}}, obj)
	if err == nil || err.Error() != want {
		t.Errorf("Verify(rsa) err = %v; want %q", err, want)
	}
}

func TestFormatSignableText(t *testing.T) {
	mod := &Module{}

	const want = "[AAECAwQFBgcICQoLDA0O] hello"
	if got := mod.formatSignableText(newSignableTextObject()); got != want {
		t.Fatalf("formatSignableText = %q; want %q", got, want)
	}
}
