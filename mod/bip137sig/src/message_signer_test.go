package src

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/api/secp256k1"
	cryptomod "github.com/astralp2p/astrald/mod/crypto"
	"github.com/btcsuite/btcd/btcec/v2"
)

func TestAppendCompactSize(t *testing.T) {
	tests := []struct {
		n    uint64
		want string
	}{
		{252, "fc"},
		{253, "fdfd00"},
		{0xffff, "fdffff"},
		{0x10000, "fe00000100"},
		{0xffffffff, "feffffffff"},
		{0x100000000, "ff0000000001000000"},
	}

	for _, tt := range tests {
		if got := hex.EncodeToString(appendCompactSize(nil, tt.n)); got != tt.want {
			t.Errorf("appendCompactSize(%#x) = %s; want %s", tt.n, got, tt.want)
		}
	}
}

func TestFormatBitcoinMessage(t *testing.T) {
	const want = "18426974636f696e205369676e6564204d6573736167653a0a0568656c6c6f"
	if got := hex.EncodeToString(formatBitcoinMessage("hello")); got != want {
		t.Fatalf("formatBitcoinMessage(hello) = %s; want %s", got, want)
	}

	msg := strings.Repeat("a", 300)
	got := formatBitcoinMessage(msg)

	prefixLen := 1 + len("Bitcoin Signed Message:\n")
	if length := got[prefixLen : prefixLen+3]; !bytes.Equal(length, []byte{0xfd, 0x2c, 0x01}) {
		t.Fatalf("300-byte message length prefix = %x; want fd2c01", length)
	}

	if body := string(got[prefixLen+3:]); body != msg {
		t.Fatalf("300-byte message body has %d bytes; want the %d-byte message", len(body), len(msg))
	}
}

func TestMessageSignerRoundTrip(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}

	tests := []struct {
		name       string
		compressed bool
		pubKey     []byte
	}{
		{"compressed", true, priv.PubKey().SerializeCompressed()},
		{"uncompressed", false, priv.PubKey().SerializeUncompressed()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := MessageSigner{key: priv, compressed: tt.compressed}.SignText(nil, "hello")
			if err != nil {
				t.Fatalf("SignText err = %v; want nil", err)
			}

			if sig.Scheme != crypto.SchemeBIP137 || len(sig.Data) != 65 {
				t.Fatalf("SignText = scheme %q, %d-byte data; want scheme %q, 65-byte data", sig.Scheme, len(sig.Data), crypto.SchemeBIP137)
			}

			pub := &crypto.PublicKey{Type: secp256k1.KeyType, Key: tt.pubKey}

			if err := (Engine{}).VerifyTextSignature(pub, sig, "hello"); err != nil {
				t.Fatalf("VerifyTextSignature(hello) err = %v; want nil", err)
			}

			if err := (Engine{}).VerifyTextSignature(pub, sig, "hellO"); !errors.Is(err, cryptomod.ErrInvalidSignature) {
				t.Fatalf("VerifyTextSignature(hellO) err = %v; want %v", err, cryptomod.ErrInvalidSignature)
			}
		})
	}
}

func TestVerifyTextSignatureErrors(t *testing.T) {
	priv, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatalf("new private key: %v", err)
	}

	sig, err := MessageSigner{key: priv, compressed: true}.SignText(nil, "hello")
	if err != nil {
		t.Fatalf("SignText: %v", err)
	}

	pub := &crypto.PublicKey{Type: secp256k1.KeyType, Key: priv.PubKey().SerializeCompressed()}

	tests := []struct {
		name string
		key  *crypto.PublicKey
		sig  *crypto.Signature
		want error
	}{
		{"short signature data", pub, &crypto.Signature{Scheme: crypto.SchemeBIP137, Data: []byte{1, 2, 3}}, cryptomod.ErrInvalidSignature},
		{"malformed public key", &crypto.PublicKey{Type: secp256k1.KeyType, Key: []byte{1, 2}}, sig, cryptomod.ErrInvalidSignature},
		{"foreign key type", &crypto.PublicKey{Type: "ed25519", Key: pub.Key}, sig, cryptomod.ErrUnsupportedKeyType},
		{"asn1 scheme", pub, &crypto.Signature{Scheme: crypto.SchemeASN1, Data: sig.Data}, cryptomod.ErrUnsupportedScheme},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := (Engine{}).VerifyTextSignature(tt.key, tt.sig, "hello"); !errors.Is(err, tt.want) {
				t.Fatalf("VerifyTextSignature err = %v; want %v", err, tt.want)
			}
		})
	}
}

func TestIsCompressedPublicKey(t *testing.T) {
	tests := []struct {
		length  int
		want    bool
		wantErr string
	}{
		{33, true, ""},
		{65, false, ""},
		{32, false, "invalid public key length: 32"},
	}

	for _, tt := range tests {
		got, err := isCompressedPublicKey(make([]byte, tt.length))

		var gotErr string
		if err != nil {
			gotErr = err.Error()
		}

		if got != tt.want || gotErr != tt.wantErr {
			t.Errorf("isCompressedPublicKey(%d bytes) = %v, %q; want %v, %q", tt.length, got, gotErr, tt.want, tt.wantErr)
		}
	}
}

func TestEngineNewTextSignerGuards(t *testing.T) {
	pub := secp256k1.PublicKey(secp256k1.New())

	tests := []struct {
		name   string
		key    *crypto.PublicKey
		scheme string
		want   error
	}{
		{"asn1 scheme", pub, crypto.SchemeASN1, cryptomod.ErrUnsupportedScheme},
		{"foreign key type", &crypto.PublicKey{Type: "ed25519", Key: pub.Key}, crypto.SchemeBIP137, cryptomod.ErrUnsupportedKeyType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, err := Engine{}.NewTextSigner(tt.key, tt.scheme)
			if signer != nil || !errors.Is(err, tt.want) {
				t.Fatalf("NewTextSigner = %v, %v; want nil, %v", signer, err, tt.want)
			}
		})
	}
}
