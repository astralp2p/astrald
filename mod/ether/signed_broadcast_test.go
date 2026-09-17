package ether

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/api/crypto"
	"github.com/astralp2p/astral-go/astral"
)

func newTestBroadcast(t *testing.T) Broadcast {
	t.Helper()

	hello := astral.String8("hello")
	return Broadcast{
		Timestamp: astral.Time(time.Unix(1700000000, 0)),
		Source:    astral.GenerateIdentity(),
		Object:    &hello,
	}
}

func assertBroadcastEqual(t *testing.T, got, want Broadcast) {
	t.Helper()

	if !time.Time(got.Timestamp).Equal(time.Time(want.Timestamp)) {
		t.Errorf("Timestamp = %v; want %v", time.Time(got.Timestamp), time.Time(want.Timestamp))
	}
	if !got.Source.IsEqual(want.Source) {
		t.Errorf("Source = %v; want %v", got.Source, want.Source)
	}

	gotObj, ok := got.Object.(*astral.String8)
	if !ok {
		t.Fatalf("Object is %T; want *astral.String8", got.Object)
	}
	if wantObj := want.Object.(*astral.String8); *gotObj != *wantObj {
		t.Errorf("Object = %q; want %q", *gotObj, *wantObj)
	}
}

func TestSignedBroadcastRoundTrip(t *testing.T) {
	orig := SignedBroadcast{Broadcast: newTestBroadcast(t)}
	hashBefore := orig.Hash()

	var buf bytes.Buffer
	written, err := orig.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got SignedBroadcast
	read, err := got.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if read != written {
		t.Errorf("ReadFrom n = %d; want %d written", read, written)
	}

	assertBroadcastEqual(t, got.Broadcast, orig.Broadcast)
	if got.Signature != nil {
		t.Errorf("Signature = %+v; want nil", got.Signature)
	}
	if hashAfter := got.Hash(); !bytes.Equal(hashAfter, hashBefore) {
		t.Errorf("Hash() after round-trip = %x; want %x", hashAfter, hashBefore)
	}
}

func TestSignedBroadcastHashCoversPayloadOnly(t *testing.T) {
	base := SignedBroadcast{Broadcast: newTestBroadcast(t)}
	hash := base.Hash()
	if len(hash) == 0 {
		t.Fatal("Hash() is empty")
	}

	signed := base
	signed.Signature = &crypto.Signature{Scheme: "x", Data: []byte{1}}
	if got := signed.Hash(); !bytes.Equal(got, hash) {
		t.Errorf("Hash() with signature = %x; want unchanged %x", got, hash)
	}

	world := astral.String8("world")
	otherObject := base
	otherObject.Object = &world
	if got := otherObject.Hash(); bytes.Equal(got, hash) {
		t.Error("Hash() unchanged after Object changed; want a different hash")
	}

	otherTime := base
	otherTime.Timestamp = astral.Time(time.Unix(1700000001, 0))
	if got := otherTime.Hash(); bytes.Equal(got, hash) {
		t.Error("Hash() unchanged after Timestamp changed; want a different hash")
	}
}

func TestBroadcastRoundTrip(t *testing.T) {
	orig := newTestBroadcast(t)

	var buf bytes.Buffer
	if _, err := orig.WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got Broadcast
	if _, err := got.ReadFrom(&buf); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	assertBroadcastEqual(t, got, orig)
}

func TestBroadcastReadTruncated(t *testing.T) {
	var buf bytes.Buffer
	if _, err := newTestBroadcast(t).WriteTo(&buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	var got Broadcast
	if _, err := got.ReadFrom(bytes.NewReader(buf.Bytes()[:10])); err == nil {
		t.Error("ReadFrom(first 10 bytes) returned nil error; want an error")
	}
}
