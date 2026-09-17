package nearby

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/astralp2p/astral-go/astral"
)

func uint8sToBytes(xs []astral.Uint8) []byte {
	out := make([]byte, len(xs))
	for i, x := range xs {
		out[i] = byte(x)
	}
	return out
}

func TestUnmaskIdentityRecoversNode(t *testing.T) {
	node, user := astral.GenerateIdentity(), astral.GenerateIdentity()

	got, err := UnmaskIdentity(MaskIdentity(node, user), user)
	if err != nil {
		t.Fatalf("UnmaskIdentity: %v", err)
	}
	if !got.IsEqual(node) {
		t.Errorf("UnmaskIdentity = %v; want %v", got, node)
	}
}

func TestMaskIdentityShape(t *testing.T) {
	node, user := astral.GenerateIdentity(), astral.GenerateIdentity()

	if n := len(MaskIdentity(node, user)); n != 33 {
		t.Errorf("len(MaskIdentity) = %d; want 33", n)
	}

	if self := MaskIdentity(node, node); !bytes.Equal(uint8sToBytes(self), make([]byte, 33)) {
		t.Errorf("MaskIdentity(x, x) = %x; want all zeros", uint8sToBytes(self))
	}
}

func TestUnmaskIdentityWithWrongUser(t *testing.T) {
	node, user, other := astral.GenerateIdentity(), astral.GenerateIdentity(), astral.GenerateIdentity()

	got, err := UnmaskIdentity(MaskIdentity(node, user), other)
	if err == nil && got.IsEqual(node) {
		t.Errorf("UnmaskIdentity with another user recovered %v; want an error or a different identity", got)
	}
}

func TestComputeCommitment(t *testing.T) {
	user, other := astral.GenerateIdentity(), astral.GenerateIdentity()

	c := ComputeCommitment(user, 42)
	if len(c) != 32 {
		t.Fatalf("len(ComputeCommitment) = %d; want 32", len(c))
	}
	if again := ComputeCommitment(user, 42); !slices.Equal(again, c) {
		t.Errorf("ComputeCommitment is not deterministic: %x then %x", uint8sToBytes(c), uint8sToBytes(again))
	}
	if slices.Equal(ComputeCommitment(user, 43), c) {
		t.Error("ComputeCommitment(user, 43) equals nonce 42; want different")
	}
	if slices.Equal(ComputeCommitment(other, 42), c) {
		t.Error("ComputeCommitment(other, 42) equals user's; want different")
	}

	h1 := sha256.Sum256(user.PublicKey().SerializeCompressed())
	want := sha256.Sum256(binary.LittleEndian.AppendUint64(h1[:], 42))
	if got := uint8sToBytes(c); !bytes.Equal(got, want[:]) {
		t.Errorf("ComputeCommitment(user, 42) = %x; want %x", got, want)
	}
}

func TestStealthHintRoundTrip(t *testing.T) {
	node, user := astral.GenerateIdentity(), astral.GenerateIdentity()
	orig := StealthHint{
		Commitment: ComputeCommitment(user, 9),
		MaskedID:   MaskIdentity(node, user),
		Nonce:      9,
	}

	var buf bytes.Buffer
	if _, err := orig.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got StealthHint
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if !slices.Equal(got.Commitment, orig.Commitment) {
		t.Errorf("Commitment = %x; want %x", uint8sToBytes(got.Commitment), uint8sToBytes(orig.Commitment))
	}
	if !slices.Equal(got.MaskedID, orig.MaskedID) {
		t.Errorf("MaskedID = %x; want %x", uint8sToBytes(got.MaskedID), uint8sToBytes(orig.MaskedID))
	}
	if got.Nonce != orig.Nonce {
		t.Errorf("Nonce = %v; want %v", got.Nonce, orig.Nonce)
	}
}
