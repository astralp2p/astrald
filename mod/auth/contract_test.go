package auth

import (
	"bytes"
	"testing"
)

func TestPermitRoundTrip(t *testing.T) {
	p := &Permit{Action: "test.action", Delegation: 3}

	var buf bytes.Buffer
	_, err := p.WriteTo(&buf)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	var bin Permit
	_, err = bin.ReadFrom(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if bin.Action != p.Action || bin.Delegation != p.Delegation {
		t.Fatalf("binary round-trip: got %+v, want %+v", bin, *p)
	}

	j, err := p.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var js Permit
	err = js.UnmarshalJSON(j)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if js.Action != p.Action || js.Delegation != p.Delegation {
		t.Fatalf("json round-trip: got %+v, want %+v", js, *p)
	}
}
