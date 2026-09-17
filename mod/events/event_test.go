package events

import (
	"bytes"
	"testing"
	"time"

	"github.com/astralp2p/astral-go/astral"
)

func roundTrip(t *testing.T, sent Event) Event {
	t.Helper()

	var buf bytes.Buffer
	written, err := sent.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if int(written) != buf.Len() {
		t.Fatalf("WriteTo reported %d bytes, wrote %d", written, buf.Len())
	}

	var got Event
	read, err := got.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if read != written {
		t.Fatalf("ReadFrom read %d bytes, WriteTo wrote %d", read, written)
	}
	return got
}

func TestEventRoundTrips(t *testing.T) {
	sent := Event{
		ID:        astral.NewNonce(),
		SourceID:  astral.GenerateIdentity(),
		Timestamp: astral.Time(time.Unix(1700000000, int64(123*time.Millisecond))),
		Data:      astral.NewString8("hello"),
	}

	got := roundTrip(t, sent)

	if got.ID != sent.ID {
		t.Errorf("ID = %v, want %v", got.ID, sent.ID)
	}
	if !got.SourceID.IsEqual(sent.SourceID) {
		t.Errorf("SourceID = %v, want %v", got.SourceID, sent.SourceID)
	}
	if !got.Timestamp.Time().Equal(sent.Timestamp.Time()) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, sent.Timestamp)
	}
	data, ok := got.Data.(*astral.String8)
	if !ok || data == nil || *data != "hello" {
		t.Errorf("Data = %#v, want *astral.String8 %q", got.Data, "hello")
	}
}

func TestEventWithoutDataRoundTrips(t *testing.T) {
	sent := Event{
		ID:        astral.NewNonce(),
		SourceID:  astral.GenerateIdentity(),
		Timestamp: astral.Now(),
	}

	if got := roundTrip(t, sent); got.Data != nil {
		t.Fatalf("Data = %#v, want nil", got.Data)
	}
}

func TestEventResolvesByTypeName(t *testing.T) {
	const name = "mod.events.event"
	obj := astral.New(name)
	if _, ok := obj.(*Event); !ok {
		t.Fatalf("astral.New(%q) returned %T, want *Event", name, obj)
	}
}
